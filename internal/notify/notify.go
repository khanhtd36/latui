// Package notify reads and marks Me's Atlassian notification feed through
// the GraphQL gateway (see ADR 0002).
package notify

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/khanhtd36/latui/internal/adf"
	"github.com/khanhtd36/latui/internal/auth"
)

var (
	//go:embed feed.graphql
	feedQuery string
	//go:embed ops.graphql
	opsQuery string
)

// Category is the Direct or Watching tab of the feed.
type Category string

const (
	Direct   Category = "direct"
	Watching Category = "watching"
)

// Product is the Atlassian product a notification's target lives in.
type Product int

const (
	Other Product = iota
	Jira
	Confluence
)

// Notification is one entry of the feed.
type Notification struct {
	ID       string
	Time     time.Time
	Unread   bool
	Category Category
	Product  Product
	Template string // messageTemplateId, such as jira-issue-comment

	Message string // "Priya Shah mentioned you on a page"
	Actor   string
	Title   string // the target's title
	URL     string
	Status  string // a Jira issue's status
	Context string // Jira issue key, or Confluence space name
	Key     string // Jira issue key, "" for other products
	Snippet string // one line of the comment or text the notification quotes
}

// Group is a row of the feed: notifications about one target, headed by
// the newest.
type Group struct {
	ID     string
	Size   int
	Unread bool // any notification in the group is unread
	Head   Notification
}

// Page is one slice of the feed.
type Page struct {
	Groups  []Group
	After   string // cursor for the next page
	HasMore bool
}

// Client talks to the notification feed of the credential's site.
type Client struct {
	Creds auth.Source
	HTTP  *http.Client
	// URL overrides the credential's GraphQL endpoint, for tests.
	URL string
}

func New(creds auth.Source) *Client {
	return &Client{Creds: creds, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Feed is a page of groups in a category; unreadOnly hides read groups.
func (c *Client) Feed(ctx context.Context, cat Category, unreadOnly bool, first int, after string) (Page, error) {
	filter := map[string]any{"categoryFilter": cat}
	if unreadOnly {
		filter["readStateFilter"] = "unread"
	}
	vars := map[string]any{"first": first, "filter": filter}
	if after != "" {
		vars["after"] = after
	}
	var data struct {
		Notifications struct {
			NotificationFeed struct {
				PageInfo pageInfo  `json:"pageInfo"`
				Nodes    []rawNode `json:"nodes"`
			} `json:"notificationFeed"`
		} `json:"notifications"`
	}
	if err := c.do(ctx, feedQuery, "LatuiFeed", vars, &data); err != nil {
		return Page{}, err
	}
	feed := data.Notifications.NotificationFeed
	page := Page{After: feed.PageInfo.EndCursor, HasMore: feed.PageInfo.HasNextPage}
	for _, n := range feed.Nodes {
		page.Groups = append(page.Groups, n.group())
	}
	return page, nil
}

// Unseen is the count the bell shows.
func (c *Client) Unseen(ctx context.Context) (int, error) {
	var data struct {
		Notifications struct {
			Count int `json:"unseenNotificationCount"`
		} `json:"notifications"`
	}
	err := c.do(ctx, opsQuery, "LatuiUnseen", map[string]any{}, &data)
	return data.Notifications.Count, err
}

// GroupIDs lists the IDs of every notification in a group, for marking
// the whole group at once.
func (c *Client) GroupIDs(ctx context.Context, g Group) ([]string, error) {
	if g.Size <= 1 {
		return []string{g.Head.ID}, nil
	}
	var ids []string
	after := ""
	for range 10 { // at most 1000
		vars := map[string]any{"groupId": g.ID, "filter": map[string]any{"categoryFilter": g.Head.Category}}
		if after != "" {
			vars["after"] = after
		}
		var data struct {
			Notifications struct {
				NotificationGroup struct {
					PageInfo pageInfo `json:"pageInfo"`
					Nodes    []struct {
						ID string `json:"notificationId"`
					} `json:"nodes"`
				} `json:"notificationGroup"`
			} `json:"notifications"`
		}
		if err := c.do(ctx, opsQuery, "LatuiGroup", vars, &data); err != nil {
			return nil, err
		}
		grp := data.Notifications.NotificationGroup
		for _, n := range grp.Nodes {
			ids = append(ids, n.ID)
		}
		if !grp.PageInfo.HasNextPage || grp.PageInfo.EndCursor == "" {
			break
		}
		after = grp.PageInfo.EndCursor
	}
	if len(ids) == 0 {
		ids = []string{g.Head.ID}
	}
	return ids, nil
}

// MarkRead marks notifications read, or unread when read is false.
func (c *Client) MarkRead(ctx context.Context, ids []string, read bool) error {
	op := "LatuiMarkRead"
	if !read {
		op = "LatuiMarkUnread"
	}
	return c.do(ctx, opsQuery, op, map[string]any{"ids": ids}, nil)
}

// MarkGroup marks every notification in a group.
func (c *Client) MarkGroup(ctx context.Context, g Group, read bool) error {
	ids, err := c.GroupIDs(ctx, g)
	if err != nil {
		return err
	}
	return c.MarkRead(ctx, ids, read)
}

// MarkAllRead marks everything in a category read.
func (c *Client) MarkAllRead(ctx context.Context, cat Category) error {
	return c.do(ctx, opsQuery, "LatuiMarkAllRead", map[string]any{"category": cat}, nil)
}

// SchemaError is a GraphQL error: most likely Atlassian changed the
// schema under latui.
type SchemaError struct{ Messages []string }

func (e *SchemaError) Error() string {
	return "notifications API: " + strings.Join(e.Messages, "; ")
}

func (c *Client) do(ctx context.Context, query, op string, vars map[string]any, out any) error {
	cred, err := c.Creds.Credential(ctx)
	if err != nil {
		return err
	}
	vars["ari"] = cred.SiteARI()
	body, err := json.Marshal(map[string]any{"query": query, "operationName": op, "variables": vars})
	if err != nil {
		return err
	}
	endpoint := cmpOr(c.URL, cred.GraphQLURL())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", cred.Header())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notifications API: %s: %s", resp.Status, firstLine(string(b)))
	}
	var env struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return fmt.Errorf("notifications API: %w", err)
	}
	if len(env.Errors) > 0 {
		e := &SchemaError{}
		for _, x := range env.Errors {
			e.Messages = append(e.Messages, x.Message)
		}
		return e
	}
	if out == nil {
		return nil
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return errors.New("notifications API: empty response")
	}
	return json.Unmarshal(env.Data, out)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type rawNode struct {
	GroupID    string   `json:"groupId"`
	GroupSize  int      `json:"groupSize"`
	ReadStates []string `json:"readStates"`
	Head       rawItem  `json:"headNotification"`
}

type rawItem struct {
	ID        string    `json:"notificationId"`
	Timestamp time.Time `json:"timestamp"`
	ReadState string    `json:"readState"`
	Category  string    `json:"category"`
	Analytics []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"analyticsAttributes"`
	Content struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		URL     string `json:"url"`
		Actor   struct {
			DisplayName string `json:"displayName"`
		} `json:"actor"`
		Entity struct {
			Title  string `json:"title"`
			URL    string `json:"url"`
			Status string `json:"status"`
		} `json:"entity"`
		Path []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"path"`
		BodyItems []struct {
			Document struct {
				Format string          `json:"format"`
				Data   json.RawMessage `json:"data"`
			} `json:"document"`
		} `json:"bodyItems"`
	} `json:"content"`
}

func (n rawNode) group() Group {
	g := Group{ID: n.GroupID, Size: max(1, n.GroupSize), Head: n.Head.notification()}
	g.Unread = g.Head.Unread
	for _, s := range n.ReadStates {
		if strings.EqualFold(s, "unread") {
			g.Unread = true
		}
	}
	return g
}

var issueKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]+-[0-9]+$`)

func (r rawItem) notification() Notification {
	c := r.Content
	n := Notification{
		ID:       r.ID,
		Time:     r.Timestamp,
		Unread:   strings.EqualFold(r.ReadState, "unread"),
		Category: Category(strings.ToLower(r.Category)),
		Message:  c.Message,
		Actor:    c.Actor.DisplayName,
		Title:    c.Entity.Title,
		URL:      cmpOr(c.URL, c.Entity.URL),
		Status:   c.Entity.Status,
	}
	for _, a := range r.Analytics {
		switch a.Key {
		case "registrationProduct":
			switch a.Value {
			case "jira":
				n.Product = Jira
			case "confluence":
				n.Product = Confluence
			}
		case "messageTemplateId":
			n.Template = a.Value
		}
	}
	if n.Product == Other {
		n.Product = productFromURL(n.URL)
	}
	if len(c.Path) > 0 {
		n.Context = c.Path[0].Title
	}
	if n.Product == Jira {
		n.Key = KeyFromURL(n.URL)
		if n.Key == "" && issueKey.MatchString(n.Context) {
			n.Key = n.Context
		}
	}
	for _, b := range c.BodyItems {
		if !strings.EqualFold(b.Document.Format, "ADF") {
			continue
		}
		if doc, err := adf.Parse(b.Document.Data); err == nil {
			if s := adf.PlainText(doc); s != "" {
				n.Snippet = s
				break
			}
		}
	}
	return n
}

func productFromURL(u string) Product {
	switch {
	case strings.Contains(u, "/wiki/"):
		return Confluence
	case strings.Contains(u, "/browse/"):
		return Jira
	}
	return Other
}

// KeyFromURL finds the issue key in a Jira link: /browse/KEY-1, or a
// selectedIssue=KEY-1 query.
func KeyFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if k := u.Query().Get("selectedIssue"); issueKey.MatchString(k) {
		return k
	}
	parts := strings.Split(u.Path, "/")
	for i, p := range parts {
		if p == "browse" && i+1 < len(parts) && issueKey.MatchString(parts[i+1]) {
			return parts[i+1]
		}
	}
	return ""
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
