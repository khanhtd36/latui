package jira

import (
	"context"
	"net/url"
	"strings"
)

// Transition is a move the workflow allows from the issue's status.
type Transition struct {
	ID   string
	Name string
	To   Status
	// NeedsResolution: the transition's screen requires a Resolution.
	NeedsResolution bool
	Resolutions     []string
	// OtherRequired are required fields latui can't fill: the transition
	// is done in the browser.
	OtherRequired []string
}

// Transitions lists the moves allowed from the issue's current status.
func (c *Client) Transitions(ctx context.Context, key string) ([]Transition, error) {
	var v struct {
		Transitions []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			To     Status `json:"to"`
			Fields map[string]struct {
				Required      bool   `json:"required"`
				HasDefault    bool   `json:"hasDefaultValue"`
				Name          string `json:"name"`
				AllowedValues []struct {
					Name string `json:"name"`
				} `json:"allowedValues"`
			} `json:"fields"`
		} `json:"transitions"`
	}
	if err := c.do(ctx, "GET", "/rest/api/3/issue/"+esc(key)+"/transitions?expand=transitions.fields", nil, &v); err != nil {
		return nil, err
	}
	out := make([]Transition, 0, len(v.Transitions))
	for _, t := range v.Transitions {
		tr := Transition{ID: t.ID, Name: t.Name, To: t.To}
		for id, f := range t.Fields {
			if !f.Required || f.HasDefault {
				continue
			}
			if id == "resolution" {
				tr.NeedsResolution = true
				for _, a := range f.AllowedValues {
					tr.Resolutions = append(tr.Resolutions, a.Name)
				}
				continue
			}
			tr.OtherRequired = append(tr.OtherRequired, cmpOr(f.Name, id))
		}
		out = append(out, tr)
	}
	return out, nil
}

// Transition moves the issue; resolution is set when not "".
func (c *Client) Transition(ctx context.Context, key, id, resolution string) error {
	body := map[string]any{"transition": map[string]string{"id": id}}
	if resolution != "" {
		body["fields"] = map[string]any{"resolution": map[string]string{"name": resolution}}
	}
	return c.do(ctx, "POST", "/rest/api/3/issue/"+esc(key)+"/transitions", body, nil)
}

func (c *Client) editFields(ctx context.Context, key string, fields map[string]any) error {
	return c.do(ctx, "PUT", "/rest/api/3/issue/"+esc(key)+"?notifyUsers=true", map[string]any{"fields": fields}, nil)
}

// SetSummary changes the issue's summary.
func (c *Client) SetSummary(ctx context.Context, key, summary string) error {
	return c.editFields(ctx, key, map[string]any{"summary": summary})
}

// SetLabels replaces the issue's labels.
func (c *Client) SetLabels(ctx context.Context, key string, labels []string) error {
	if labels == nil {
		labels = []string{}
	}
	return c.editFields(ctx, key, map[string]any{"labels": labels})
}

// SetComponents replaces the issue's components, by ID.
func (c *Client) SetComponents(ctx context.Context, key string, ids []string) error {
	list := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		list = append(list, map[string]string{"id": id})
	}
	return c.editFields(ctx, key, map[string]any{"components": list})
}

// Assign gives the issue to a user; accountID "" unassigns it.
func (c *Client) Assign(ctx context.Context, key, accountID string) error {
	var body map[string]any
	if accountID == "" {
		body = map[string]any{"accountId": nil}
	} else {
		body = map[string]any{"accountId": accountID}
	}
	return c.do(ctx, "PUT", "/rest/api/3/issue/"+esc(key)+"/assignee", body, nil)
}

// AssignableUsers finds people the issue can be assigned to.
func (c *Client) AssignableUsers(ctx context.Context, key, query string) ([]User, error) {
	q := url.Values{"issueKey": {key}, "query": {query}, "maxResults": {"30"}}
	var users []User
	err := c.do(ctx, "GET", "/rest/api/3/user/assignable/search?"+q.Encode(), nil, &users)
	return users, err
}

// LabelSuggestions are existing labels matching prefix.
func (c *Client) LabelSuggestions(ctx context.Context, prefix string) ([]string, error) {
	q := url.Values{"fieldName": {"labels"}, "fieldValue": {prefix}}
	var v struct {
		Results []struct {
			Value string `json:"value"`
		} `json:"results"`
	}
	if err := c.do(ctx, "GET", "/rest/api/3/jql/autocompletedata/suggestions?"+q.Encode(), nil, &v); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(v.Results))
	for _, r := range v.Results {
		// Values come back with the match in <b> tags on some sites.
		s := strings.NewReplacer("<b>", "", "</b>", "").Replace(r.Value)
		out = append(out, s)
	}
	return out, nil
}

// Myself is the signed-in user.
func (c *Client) Myself(ctx context.Context) (User, error) {
	var u User
	err := c.do(ctx, "GET", "/rest/api/3/myself", nil, &u)
	return u, err
}
