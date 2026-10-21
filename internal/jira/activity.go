package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/khanhtd36/latui/internal/adf"
)

// Comment is one comment on an issue.
type Comment struct {
	ID      string
	Author  User
	Created string
	Updated string
	Body    adf.Node
}

// Markdown is the comment's body as markdown.
func (c Comment) Markdown() string { return adf.ToMarkdown(c.Body) }

// maxActivity caps how many comments or history entries latui loads.
const maxActivity = 500

// Comments lists an issue's comments, newest first.
func (c *Client) Comments(ctx context.Context, key string) ([]Comment, error) {
	var out []Comment
	for start := 0; start < maxActivity; {
		var v struct {
			Total    int `json:"total"`
			Comments []struct {
				ID      string          `json:"id"`
				Author  User            `json:"author"`
				Created string          `json:"created"`
				Updated string          `json:"updated"`
				Body    json.RawMessage `json:"body"`
			} `json:"comments"`
		}
		q := url.Values{"orderBy": {"-created"}, "startAt": {strconv.Itoa(start)}, "maxResults": {"100"}}
		if err := c.do(ctx, "GET", "/rest/api/3/issue/"+esc(key)+"/comment?"+q.Encode(), nil, &v); err != nil {
			return nil, err
		}
		for _, cm := range v.Comments {
			body, _ := adf.Parse(cm.Body)
			out = append(out, Comment{ID: cm.ID, Author: cm.Author, Created: cm.Created, Updated: cm.Updated, Body: body})
		}
		start += len(v.Comments)
		if len(v.Comments) == 0 || start >= v.Total {
			break
		}
	}
	// orderBy is a hint some sites ignore; sort to be sure.
	sort.SliceStable(out, func(i, j int) bool { return Time(out[i].Created).After(Time(out[j].Created)) })
	return out, nil
}

// AddComment posts a comment written in markdown, with the mentions typed
// in it as subs.
func (c *Client) AddComment(ctx context.Context, key, markdown string, subs []adf.Sub) error {
	return c.do(ctx, "POST", "/rest/api/3/issue/"+esc(key)+"/comment",
		map[string]any{"body": adf.FromMarkdownWith(markdown, subs)}, nil)
}

// Change is one field change in an issue's history.
type Change struct {
	Field string
	From  string
	To    string
}

// HistoryEntry is the changes one person made at one time.
type HistoryEntry struct {
	Author  User
	Created string
	Changes []Change
}

// History lists an issue's field changes, newest first.
func (c *Client) History(ctx context.Context, key string) ([]HistoryEntry, error) {
	var out []HistoryEntry
	for start := 0; start < maxActivity; {
		var v struct {
			Total  int  `json:"total"`
			IsLast bool `json:"isLast"`
			Values []struct {
				Author  User   `json:"author"`
				Created string `json:"created"`
				Items   []struct {
					Field      string `json:"field"`
					FromString string `json:"fromString"`
					ToString   string `json:"toString"`
				} `json:"items"`
			} `json:"values"`
		}
		q := url.Values{"startAt": {strconv.Itoa(start)}, "maxResults": {"100"}}
		if err := c.do(ctx, "GET", "/rest/api/3/issue/"+esc(key)+"/changelog?"+q.Encode(), nil, &v); err != nil {
			return nil, err
		}
		for _, h := range v.Values {
			e := HistoryEntry{Author: h.Author, Created: h.Created}
			for _, it := range h.Items {
				e.Changes = append(e.Changes, Change{Field: it.Field, From: it.FromString, To: it.ToString})
			}
			out = append(out, e)
		}
		start += len(v.Values)
		if len(v.Values) == 0 || v.IsLast || start >= v.Total {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return Time(out[i].Created).After(Time(out[j].Created)) })
	return out, nil
}

// Download opens an attachment's content: its body, which the caller closes,
// and its size. Atlassian answers with a redirect to its media service, which
// the client follows; there is no overall time limit, only ctx, so a big file
// on a slow line can finish.
func (c *Client) Download(ctx context.Context, a Attachment) (io.ReadCloser, int64, error) {
	cred, err := c.Creds.Credential(ctx)
	if err != nil {
		return nil, 0, err
	}
	base := c.Base
	if base == "" {
		base = cred.JiraBase()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/rest/api/3/attachment/content/"+esc(a.ID), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", cred.Header())
	hc := &http.Client{Transport: c.HTTP.Transport}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		return nil, 0, apiError(resp.StatusCode, b)
	}
	return resp.Body, resp.ContentLength, nil
}
