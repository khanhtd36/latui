package ui

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/khanhtd36/latui/internal/jira"
)

func (f *fakeJira) Search(_ context.Context, jql string) ([]jira.Row, error) {
	for prefix, rows := range f.rows {
		if strings.HasPrefix(jql, prefix) {
			return rows, nil
		}
	}
	return nil, nil
}

func (f *fakeJira) FavouriteFilters(context.Context) ([]jira.Filter, error) {
	return []jira.Filter{{ID: "1", Name: "QA tickets to verify", JQL: "filter-qa"}}, nil
}

func (f *fakeJira) RecentProjects(context.Context) ([]jira.Project, error) {
	return []jira.Project{{Key: "QA", Name: "Quality"}, {Key: "DEMO", Name: "Demo"}}, nil
}

func (f *fakeJira) CreateTypes(context.Context, string) ([]jira.IssueType, error) {
	return []jira.IssueType{{ID: "1", Name: "Bug"}, {ID: "2", Name: "Task"}, {ID: "3", Name: "Sub-task", Subtask: true}}, nil
}

func (f *fakeJira) CreateFields(_ context.Context, project, _ string) ([]jira.CreateField, error) {
	fs := []jira.CreateField{
		{ID: "summary", Name: "Summary", Required: true},
		{ID: "labels", Name: "Labels"},
		{ID: "assignee", Name: "Assignee"},
		{ID: "priority", Name: "Priority", AllowedValues: []json.RawMessage{[]byte(`{"id":"3","name":"Medium"}`), []byte(`{"id":"2","name":"High"}`)}},
	}
	if project == "QA" {
		fs = append(fs, jira.CreateField{ID: "components", Name: "Components", AllowedValues: []json.RawMessage{[]byte(`{"id":"10","name":"Backend"}`)}})
	}
	return fs, nil
}

func (f *fakeJira) TypeStatuses(context.Context, string) (map[string][]jira.StatusOption, error) {
	return map[string][]jira.StatusOption{"1": {{Name: "To Do"}, {Name: "In Progress"}}}, nil
}

func (f *fakeJira) CreateIssue(_ context.Context, n jira.NewIssue) (string, error) {
	f.created = append(f.created, n)
	return n.Project + "-" + string(rune('0'+len(f.created))), nil
}

func (f *fakeJira) MoveTo(_ context.Context, key, to string) (bool, error) {
	f.moved = key + "→" + to
	return true, nil
}

func (f *fakeJira) ProjectUsers(context.Context, string, string) ([]jira.User, error) {
	return []jira.User{{AccountID: "u2", DisplayName: "Lee Park"}}, nil
}

func (f *fakeJira) OpenEpics(context.Context, string) ([]jira.Row, error) {
	return []jira.Row{{Key: "QA-1", Summary: "Faster exports"}}, nil
}
