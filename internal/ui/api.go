package ui

import (
	"context"

	"github.com/khanhtd36/latui/internal/auth"
	"github.com/khanhtd36/latui/internal/jira"
	"github.com/khanhtd36/latui/internal/notify"
)

// jiraAPI is what the UI asks of Jira; *jira.Client in the app, fakes in
// tests.
type jiraAPI interface {
	Site(ctx context.Context) (auth.Credential, error)
	Myself(ctx context.Context) (jira.User, error)
	Issue(ctx context.Context, key string) (*jira.Issue, error)
	Comments(ctx context.Context, key string) ([]jira.Comment, error)
	History(ctx context.Context, key string) ([]jira.HistoryEntry, error)
	Transitions(ctx context.Context, key string) ([]jira.Transition, error)
	Transition(ctx context.Context, key, id, resolution string) error
	AddComment(ctx context.Context, key, markdown string) error
	SetSummary(ctx context.Context, key, summary string) error
	SetLabels(ctx context.Context, key string, labels []string) error
	SetComponents(ctx context.Context, key string, ids []string) error
	Assign(ctx context.Context, key, accountID string) error
	AssignableUsers(ctx context.Context, key, query string) ([]jira.User, error)
	LabelSuggestions(ctx context.Context, prefix string) ([]string, error)
	Search(ctx context.Context, jql string) ([]jira.Row, error)
	FavouriteFilters(ctx context.Context) ([]jira.Filter, error)
	RecentProjects(ctx context.Context) ([]jira.Project, error)
	CreateTypes(ctx context.Context, project string) ([]jira.IssueType, error)
	CreateFields(ctx context.Context, project, typeID string) ([]jira.CreateField, error)
	TypeStatuses(ctx context.Context, project string) (map[string][]jira.StatusOption, error)
	CreateIssue(ctx context.Context, n jira.NewIssue) (string, error)
	MoveTo(ctx context.Context, key, to string) (bool, error)
	ProjectUsers(ctx context.Context, project, query string) ([]jira.User, error)
	OpenEpics(ctx context.Context, project string) ([]jira.Row, error)
}

// feedAPI is what the UI asks of the notification feed.
type feedAPI interface {
	Feed(ctx context.Context, cat notify.Category, unreadOnly bool, first int, after string) (notify.Page, error)
	MarkGroup(ctx context.Context, g notify.Group, read bool) error
	MarkAllRead(ctx context.Context, cat notify.Category) error
}
