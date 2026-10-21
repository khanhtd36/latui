// Package teams searches Atlassian Teams and lists their members through the
// GraphQL gateway, with an API token: acli's OAuth token does not carry the
// scope Teams needs (view:team:teams). The token comes from the environment
// (LATUI_TEAMS_TOKEN, and LATUI_TEAMS_EMAIL when it is not the sign-in's own
// account) or from the OS secure store, where `latui teams login` or the
// mention picker put it.
package teams

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/khanhtd36/latui/internal/auth"
)

// Team is one Atlassian Team.
type Team struct {
	ID      string // the id in the team's page link: the ARI's last part
	Name    string
	Members int // how many people, as the search counts them
}

// Member is one person of a team.
type Member struct{ ID, Name string }

// State is whether teams can be looked up.
type State int

const (
	// Off: no token is set or stored.
	Off State = iota
	// Ready: there is a token and it has worked, or has not been tried.
	Ready
	// Rejected: the gateway turned the token down.
	Rejected
)

// ErrNotSetUp is a lookup asked for with no token.
var ErrNotSetUp = errors.New("teams: no token set up")

// Client talks to the gateway as the token's user.
type Client struct {
	Site   auth.Source // the sign-in that says which site and cloud ID
	Getenv func(string) string
	Store  Store
	HTTP   *http.Client
	URL    string // replaces the gateway, for tests

	mu       sync.Mutex
	orgID    string
	loadedAt string // the site the stored login was read for
	stored   *Login
	rejected bool
}

// New is the Teams lookup for a sign-in, reading the environment first and
// then the OS secure store.
func New(site auth.Source, getenv func(string) string, store Store) *Client {
	if store == nil {
		store = KeyringStore{}
	}
	return &Client{Site: site, Getenv: getenv, Store: store, HTTP: http.DefaultClient}
}

// login is the token to use for cred's site: the environment's, else the
// stored one.
func (c *Client) login(cred auth.Credential) (Login, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if tok := c.Getenv("LATUI_TEAMS_TOKEN"); tok != "" {
		return Login{Email: orDefault(c.Getenv("LATUI_TEAMS_EMAIL"), cred.Email), Token: tok}, true
	}
	if c.loadedAt != cred.Site {
		c.loadedAt, c.stored = cred.Site, nil
		if l, ok, err := c.Store.Load(cred.Site); err == nil && ok {
			c.stored = &l
		}
	}
	if c.stored == nil {
		return Login{}, false
	}
	return Login{Email: orDefault(c.stored.Email, cred.Email), Token: c.stored.Token}, true
}

func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// State says whether teams can be looked up; it reads the sign-in and the
// secure store, never the network.
func (c *Client) State(ctx context.Context) State {
	cred, err := c.Site.Credential(ctx)
	if err != nil {
		return Off
	}
	if _, ok := c.login(cred); !ok {
		return Off
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rejected {
		return Rejected
	}
	return Ready
}

const (
	teamARI = "ari:cloud:identity::team/"
	orgARI  = "ari:cloud:platform::org/"
)

type gqlError struct{ Message string }

func (c *Client) call(ctx context.Context, cred auth.Credential, lg Login, op, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "operationName": op, "variables": vars})
	if err != nil {
		return err
	}
	endpoint := c.URL
	if endpoint == "" {
		endpoint = "https://" + cred.Site + "/gateway/api/graphql"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(lg.Email+":"+lg.Token)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		c.setRejected(true)
		return fmt.Errorf("teams: %s: the token was turned down", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("teams: %s", resp.Status)
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []gqlError      `json:"errors"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return fmt.Errorf("teams: %w", err)
	}
	// A warning about an unnamed operation rides along with good data.
	var failures []string
	for _, e := range env.Errors {
		if !strings.HasPrefix(e.Message, "Warning") {
			failures = append(failures, e.Message)
		}
	}
	if len(failures) > 0 {
		if strings.Contains(failures[0], "authoris") || strings.Contains(failures[0], "scope") {
			c.setRejected(true)
		}
		return fmt.Errorf("teams: %s", failures[0])
	}
	c.setRejected(false)
	return json.Unmarshal(env.Data, out)
}

func (c *Client) setRejected(v bool) {
	c.mu.Lock()
	c.rejected = v
	c.mu.Unlock()
}

// org is the organization the site belongs to, asked once.
func (c *Client) org(ctx context.Context, cred auth.Credential, lg Login) (string, error) {
	c.mu.Lock()
	id := c.orgID
	c.mu.Unlock()
	if id != "" {
		return id, nil
	}
	var v struct {
		Tenants []struct {
			OrgID string `json:"orgId"`
		} `json:"tenantContexts"`
	}
	err := c.call(ctx, cred, lg, "TeamsOrg", `query TeamsOrg($cloudIds: [ID!]!) { tenantContexts(cloudIds: $cloudIds) { orgId } }`,
		map[string]any{"cloudIds": []string{cred.CloudID}}, &v)
	if err != nil {
		return "", err
	}
	if len(v.Tenants) == 0 || v.Tenants[0].OrgID == "" {
		return "", errors.New("teams: no organization for this site")
	}
	c.mu.Lock()
	c.orgID = v.Tenants[0].OrgID
	c.mu.Unlock()
	return v.Tenants[0].OrgID, nil
}

// Login checks a token with the gateway and, if it works, keeps it in the OS
// secure store: a wrong token is never stored. An empty email is the
// sign-in's own.
func (c *Client) Login(ctx context.Context, email, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("teams: no token given")
	}
	cred, err := c.Site.Credential(ctx)
	if err != nil {
		return err
	}
	lg := Login{Email: orDefault(email, cred.Email), Token: token}
	c.mu.Lock()
	c.orgID = ""
	c.mu.Unlock()
	if _, err := c.org(ctx, cred, lg); err != nil {
		return err
	}
	stored := Login{Email: email, Token: token}
	if err := c.Store.Save(cred.Site, stored); err != nil {
		return err
	}
	c.mu.Lock()
	c.loadedAt, c.stored, c.rejected = cred.Site, &stored, false
	c.mu.Unlock()
	return nil
}

// Logout removes the stored token. One set in the environment stays.
func (c *Client) Logout(ctx context.Context) error {
	cred, err := c.Site.Credential(ctx)
	if err != nil {
		return err
	}
	if err := c.Store.Delete(cred.Site); err != nil {
		return err
	}
	c.mu.Lock()
	c.loadedAt, c.stored, c.rejected = cred.Site, nil, false
	c.mu.Unlock()
	return nil
}

const searchQuery = `query TeamsSearch($org: ID!, $site: String!, $q: String) {
  team { teamSearchV2(organizationId: $org, siteId: $site, filter: {query: $q}, first: 15, searchFields: [NAME]) {
    nodes { memberCount team { id displayName } }
  } }
}`

type searchResult struct {
	Team struct {
		Search struct {
			Nodes []struct {
				MemberCount int `json:"memberCount"`
				Team        struct {
					ID   string `json:"id"`
					Name string `json:"displayName"`
				} `json:"team"`
			} `json:"nodes"`
		} `json:"teamSearchV2"`
	} `json:"team"`
}

// Search finds teams whose name matches query.
func (c *Client) Search(ctx context.Context, query string) ([]Team, error) {
	cred, err := c.Site.Credential(ctx)
	if err != nil {
		return nil, err
	}
	lg, ok := c.login(cred)
	if !ok {
		return nil, ErrNotSetUp
	}
	org, err := c.org(ctx, cred, lg)
	if err != nil {
		return nil, err
	}
	var v searchResult
	vars := map[string]any{"org": orgARI + org, "site": cred.CloudID, "q": query}
	if err := c.call(ctx, cred, lg, "TeamsSearch", searchQuery, vars, &v); err != nil {
		// Some sites want the bare organization ID.
		vars["org"] = org
		if err2 := c.call(ctx, cred, lg, "TeamsSearch", searchQuery, vars, &v); err2 != nil {
			return nil, err
		}
	}
	out := make([]Team, 0, len(v.Team.Search.Nodes))
	for _, n := range v.Team.Search.Nodes {
		out = append(out, Team{ID: strings.TrimPrefix(n.Team.ID, teamARI), Name: n.Team.Name, Members: n.MemberCount})
	}
	return out, nil
}

const membersQuery = `query TeamMembers($id: ID!, $site: String!) {
  team { teamV2(id: $id, siteId: $site) { members { nodes { member { accountId name } } } } }
}`

// Members lists everyone in a team.
func (c *Client) Members(ctx context.Context, teamID string) ([]Member, error) {
	cred, err := c.Site.Credential(ctx)
	if err != nil {
		return nil, err
	}
	lg, ok := c.login(cred)
	if !ok {
		return nil, ErrNotSetUp
	}
	var v struct {
		Team struct {
			Team struct {
				Members struct {
					Nodes []struct {
						Member struct {
							AccountID string `json:"accountId"`
							Name      string `json:"name"`
						} `json:"member"`
					} `json:"nodes"`
				} `json:"members"`
			} `json:"teamV2"`
		} `json:"team"`
	}
	err = c.call(ctx, cred, lg, "TeamMembers", membersQuery, map[string]any{"id": teamARI + teamID, "site": cred.CloudID}, &v)
	if err != nil {
		return nil, err
	}
	var out []Member
	for _, n := range v.Team.Team.Members.Nodes {
		if n.Member.AccountID != "" {
			out = append(out, Member{ID: n.Member.AccountID, Name: n.Member.Name})
		}
	}
	return out, nil
}
