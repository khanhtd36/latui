package jira

import (
	"context"
	"fmt"
	"strings"
)

// Row is an issue as lists show it: enough to draw a row and act on it.
type Row struct {
	Key       string
	Summary   string
	Status    Status
	IssueType string
	Priority  string
	Parent    string // the parent's key
	Due       string
	Updated   string
	Project   string
}

// rowFields are the fields a Row needs.
var rowFields = []string{"summary", "status", "issuetype", "priority", "parent", "duedate", "updated", "project"}

// maxRows caps how many issues a search loads.
const maxRows = 1000

// Search runs JQL and returns the issues as rows, in the JQL's order.
func (c *Client) Search(ctx context.Context, jql string) ([]Row, error) {
	var rows []Row
	token := ""
	for len(rows) < maxRows {
		body := map[string]any{"jql": jql, "fields": rowFields, "maxResults": 100}
		if token != "" {
			body["nextPageToken"] = token
		}
		var v struct {
			Issues []struct {
				Key    string `json:"key"`
				Fields *struct {
					Summary   string `json:"summary"`
					Status    Status `json:"status"`
					IssueType Named  `json:"issuetype"`
					Priority  *Named `json:"priority"`
					Parent    *struct {
						Key string `json:"key"`
					} `json:"parent"`
					Due     string `json:"duedate"`
					Updated string `json:"updated"`
					Project struct {
						Key string `json:"key"`
					} `json:"project"`
				} `json:"fields"` // left out when every asked field is empty
			} `json:"issues"`
			NextPageToken string `json:"nextPageToken"`
			IsLast        bool   `json:"isLast"`
		}
		if err := c.do(ctx, "POST", "/rest/api/3/search/jql", body, &v); err != nil {
			return nil, err
		}
		for _, is := range v.Issues {
			r := Row{Key: is.Key, Project: ProjectOf(is.Key)}
			if f := is.Fields; f != nil {
				r.Summary, r.Status, r.IssueType = f.Summary, f.Status, f.IssueType.Name
				r.Due, r.Updated = f.Due, f.Updated
				if f.Priority != nil {
					r.Priority = f.Priority.Name
				}
				if f.Parent != nil {
					r.Parent = f.Parent.Key
				}
				if f.Project.Key != "" {
					r.Project = f.Project.Key
				}
			}
			rows = append(rows, r)
		}
		if v.IsLast || v.NextPageToken == "" || len(v.Issues) == 0 {
			break
		}
		token = v.NextPageToken
	}
	return rows, nil
}

// Filter is a saved Jira filter.
type Filter struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	JQL  string `json:"jql"`
}

// FavouriteFilters are the filters Me starred.
func (c *Client) FavouriteFilters(ctx context.Context) ([]Filter, error) {
	var fs []Filter
	err := c.do(ctx, "GET", "/rest/api/3/filter/favourite", nil, &fs)
	return fs, err
}

// Project is a Jira project.
type Project struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// RecentProjects are the projects Me used lately, most recent first.
func (c *Client) RecentProjects(ctx context.Context) ([]Project, error) {
	var ps []Project
	err := c.do(ctx, "GET", "/rest/api/3/project/recent", nil, &ps)
	return ps, err
}

// QuoteJQL quotes a value for JQL.
func QuoteJQL(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// OpenEpics lists a project's Epics that aren't Done, for picking a parent.
func (c *Client) OpenEpics(ctx context.Context, project string) ([]Row, error) {
	return c.Search(ctx, fmt.Sprintf("project = %s AND issuetype = Epic AND statusCategory != Done ORDER BY updated DESC", QuoteJQL(project)))
}
