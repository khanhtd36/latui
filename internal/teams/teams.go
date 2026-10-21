// Package teams searches Atlassian Teams and lists their members through the
// GraphQL gateway, with an API token: acli's OAuth token does not carry the
// scope Teams needs (view:team:teams), so the lookup is optional and off until
// LATUI_TEAMS_EMAIL and LATUI_TEAMS_TOKEN are set.
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

// Client talks to the gateway as the token's user.
type Client struct {
	Site         auth.Source // the sign-in that says which site and cloud ID
	Email, Token string
	HTTP         *http.Client
	URL          string // replaces the gateway, for tests

	mu    sync.Mutex
	orgID string
}

// FromEnv is the client for LATUI_TEAMS_EMAIL and LATUI_TEAMS_TOKEN, nil when
// either is missing.
func FromEnv(site auth.Source, getenv func(string) string) *Client {
	email, token := getenv("LATUI_TEAMS_EMAIL"), getenv("LATUI_TEAMS_TOKEN")
	if email == "" || token == "" {
		return nil
	}
	return &Client{Site: site, Email: email, Token: token, HTTP: http.DefaultClient}
}

// Available says whether teams can be looked up; a nil Client has none.
func (c *Client) Available() bool { return c != nil }

const (
	teamARI = "ari:cloud:identity::team/"
	orgARI  = "ari:cloud:platform::org/"
)

type gqlError struct{ Message string }

func (c *Client) call(ctx context.Context, cred auth.Credential, op, query string, vars map[string]any, out any) error {
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
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.Email+":"+c.Token)))
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
		return fmt.Errorf("teams: %s", failures[0])
	}
	return json.Unmarshal(env.Data, out)
}

// org is the organization the site belongs to, asked once.
func (c *Client) org(ctx context.Context, cred auth.Credential) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.orgID != "" {
		return c.orgID, nil
	}
	var v struct {
		Tenants []struct {
			OrgID string `json:"orgId"`
		} `json:"tenantContexts"`
	}
	err := c.call(ctx, cred, "TeamsOrg", `query TeamsOrg($cloudIds: [ID!]!) { tenantContexts(cloudIds: $cloudIds) { orgId } }`,
		map[string]any{"cloudIds": []string{cred.CloudID}}, &v)
	if err != nil {
		return "", err
	}
	if len(v.Tenants) == 0 || v.Tenants[0].OrgID == "" {
		return "", errors.New("teams: no organization for this site")
	}
	c.orgID = v.Tenants[0].OrgID
	return c.orgID, nil
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
	org, err := c.org(ctx, cred)
	if err != nil {
		return nil, err
	}
	var v searchResult
	vars := map[string]any{"org": orgARI + org, "site": cred.CloudID, "q": query}
	if err := c.call(ctx, cred, "TeamsSearch", searchQuery, vars, &v); err != nil {
		// Some sites want the bare organization ID.
		vars["org"] = org
		if err2 := c.call(ctx, cred, "TeamsSearch", searchQuery, vars, &v); err2 != nil {
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
	err = c.call(ctx, cred, "TeamMembers", membersQuery, map[string]any{"id": teamARI + teamID, "site": cred.CloudID}, &v)
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
