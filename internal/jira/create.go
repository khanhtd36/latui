package jira

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"

	"github.com/khanhtd36/latui/internal/adf"
)

// IssueType is a type issues can be created with in a project.
type IssueType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Subtask        bool   `json:"subtask"`
	HierarchyLevel int    `json:"hierarchyLevel"`
}

// CreateTypes lists the issue types Me can create in a project.
func (c *Client) CreateTypes(ctx context.Context, project string) ([]IssueType, error) {
	var v struct {
		IssueTypes []IssueType `json:"issueTypes"`
	}
	err := c.do(ctx, "GET", "/rest/api/3/issue/createmeta/"+esc(project)+"/issuetypes?maxResults=100", nil, &v)
	return v.IssueTypes, err
}

// CreateField is one field of a project and type's create screen.
type CreateField struct {
	ID            string `json:"fieldId"`
	Name          string `json:"name"`
	Required      bool   `json:"required"`
	HasDefault    bool   `json:"hasDefaultValue"`
	Schema        FieldSchema
	AllowedValues []json.RawMessage `json:"allowedValues"`
}

// Option is an allowed value: a priority, component or version.
type Option struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Options decodes a field's allowed values.
func (f CreateField) Options() []Option {
	out := make([]Option, 0, len(f.AllowedValues))
	for _, raw := range f.AllowedValues {
		var o struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Value string `json:"value"`
		}
		if json.Unmarshal(raw, &o) == nil && o.ID != "" {
			out = append(out, Option{ID: o.ID, Name: cmpOr(o.Name, o.Value)})
		}
	}
	return out
}

// CreateFields lists the fields of the create screen for a project and type.
func (c *Client) CreateFields(ctx context.Context, project, typeID string) ([]CreateField, error) {
	var out []CreateField
	for start := 0; start < 1000; {
		var v struct {
			Total  int `json:"total"`
			Fields []struct {
				CreateField
				Schema FieldSchema `json:"schema"`
			} `json:"fields"`
		}
		path := "/rest/api/3/issue/createmeta/" + esc(project) + "/issuetypes/" + esc(typeID) +
			"?maxResults=100&startAt=" + strconv.Itoa(start)
		if err := c.do(ctx, "GET", path, nil, &v); err != nil {
			return nil, err
		}
		for _, f := range v.Fields {
			cf := f.CreateField
			cf.Schema = f.Schema
			out = append(out, cf)
		}
		start += len(v.Fields)
		if len(v.Fields) == 0 || start >= v.Total {
			break
		}
	}
	return out, nil
}

// StatusOption is a status an issue of some type can be in.
type StatusOption struct {
	Name     string
	Category string
}

// TypeStatuses lists the statuses of each issue type in a project, by type ID.
func (c *Client) TypeStatuses(ctx context.Context, project string) (map[string][]StatusOption, error) {
	var v []struct {
		ID       string   `json:"id"`
		Statuses []Status `json:"statuses"`
	}
	if err := c.do(ctx, "GET", "/rest/api/3/project/"+esc(project)+"/statuses", nil, &v); err != nil {
		return nil, err
	}
	out := make(map[string][]StatusOption, len(v))
	for _, t := range v {
		for _, s := range t.Statuses {
			out[t.ID] = append(out[t.ID], StatusOption{Name: s.Name, Category: s.Category.Key})
		}
	}
	return out, nil
}

// NewIssue is what the create form sends; empty values are left out.
type NewIssue struct {
	Project     string // key
	TypeID      string
	Summary     string
	Description string // markdown
	Assignee    string // account ID; "" lets Jira pick (Automatic)
	Labels      []string
	Components  []string // IDs
	PriorityID  string
	Parent      string // key
	Due         string // 2006-01-02
	// Users are user-list custom fields, such as Tester: field ID → account IDs.
	Users map[string][]string
}

// fields is the create request's fields object.
func (n NewIssue) fields() map[string]any {
	f := map[string]any{
		"project":   map[string]string{"key": n.Project},
		"issuetype": map[string]string{"id": n.TypeID},
		"summary":   n.Summary,
	}
	if n.Description != "" {
		f["description"] = adf.FromMarkdown(n.Description)
	}
	if n.Assignee != "" {
		f["assignee"] = map[string]string{"accountId": n.Assignee}
	}
	if len(n.Labels) > 0 {
		f["labels"] = n.Labels
	}
	if len(n.Components) > 0 {
		cs := make([]map[string]string, 0, len(n.Components))
		for _, id := range n.Components {
			cs = append(cs, map[string]string{"id": id})
		}
		f["components"] = cs
	}
	if n.PriorityID != "" {
		f["priority"] = map[string]string{"id": n.PriorityID}
	}
	if n.Parent != "" {
		f["parent"] = map[string]string{"key": n.Parent}
	}
	if n.Due != "" {
		f["duedate"] = n.Due
	}
	for id, users := range n.Users {
		if len(users) == 0 {
			continue
		}
		us := make([]map[string]string, 0, len(users))
		for _, u := range users {
			us = append(us, map[string]string{"accountId": u})
		}
		f[id] = us
	}
	return f
}

// CreateIssue creates an issue and returns its key.
func (c *Client) CreateIssue(ctx context.Context, n NewIssue) (string, error) {
	var v struct {
		Key string `json:"key"`
	}
	err := c.do(ctx, "POST", "/rest/api/3/issue", map[string]any{"fields": n.fields()}, &v)
	return v.Key, err
}

// MoveTo transitions an issue to the status named to, if a transition
// leads there; it reports whether one did.
func (c *Client) MoveTo(ctx context.Context, key, to string) (bool, error) {
	ts, err := c.Transitions(ctx, key)
	if err != nil {
		return false, err
	}
	for _, t := range ts {
		if t.To.Name == to && !t.NeedsResolution && len(t.OtherRequired) == 0 {
			return true, c.Transition(ctx, key, t.ID, "")
		}
	}
	return false, nil
}

// ProjectUsers finds people who can be assigned issues in a project.
func (c *Client) ProjectUsers(ctx context.Context, project, query string) ([]User, error) {
	q := url.Values{"project": {project}, "query": {query}, "maxResults": {"30"}}
	var users []User
	err := c.do(ctx, "GET", "/rest/api/3/user/assignable/search?"+q.Encode(), nil, &users)
	return users, err
}
