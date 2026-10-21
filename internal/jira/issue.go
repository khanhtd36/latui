package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/khanhtd36/latui/internal/adf"
)

// User is a Jira user, as fields and searches return one.
type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Email       string `json:"emailAddress"`
	Active      bool   `json:"active"`
}

// Named is a field value Jira identifies by ID and names, such as an
// issue type, priority or version.
type Named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Status is an issue's workflow status.
type Status struct {
	Name     string `json:"name"`
	Category struct {
		Key string `json:"key"` // new, indeterminate, done
	} `json:"statusCategory"`
}

// LinkedIssue is an issue Named by another: a parent, subtask or link.
type LinkedIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary   string `json:"summary"`
		Status    Status `json:"status"`
		IssueType Named  `json:"issuetype"`
	} `json:"fields"`
}

// IssueLink is one row of "is blocked by", "relates to" and so on.
type IssueLink struct {
	Type struct {
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	Inward  *LinkedIssue `json:"inwardIssue"`
	Outward *LinkedIssue `json:"outwardIssue"`
}

// Attachment is a file on an issue.
type Attachment struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	Created  string `json:"created"`
	Author   User   `json:"author"`
	Content  string `json:"content"`
	MimeType string `json:"mimeType"`
}

// Fields are the standard fields latui shows by name.
type Fields struct {
	Summary     string                     `json:"summary"`
	Status      Status                     `json:"status"`
	IssueType   Named                      `json:"issuetype"`
	Priority    *Named                     `json:"priority"`
	Resolution  *Named                     `json:"resolution"`
	Assignee    *User                      `json:"assignee"`
	Reporter    *User                      `json:"reporter"`
	Labels      []string                   `json:"labels"`
	Components  []Named                    `json:"components"`
	FixVersions []Named                    `json:"fixVersions"`
	Project     struct{ Key, Name string } `json:"project"`
	Parent      *LinkedIssue               `json:"parent"`
	Subtasks    []LinkedIssue              `json:"subtasks"`
	Links       []IssueLink                `json:"issuelinks"`
	Attachments []Attachment               `json:"attachment"`
	Created     string                     `json:"created"`
	Updated     string                     `json:"updated"`
	Due         string                     `json:"duedate"`
	Description json.RawMessage            `json:"description"`
}

// EditField is what editmeta says about one field: whether it can be
// edited here and, for some, the values allowed.
type EditField struct {
	Name          string            `json:"name"`
	Required      bool              `json:"required"`
	AllowedValues []json.RawMessage `json:"allowedValues"`
	Operations    []string          `json:"operations"`
}

// Issue is everything Issue detail shows, but comments and history.
type Issue struct {
	ID       string
	Key      string
	Fields   Fields
	Raw      map[string]json.RawMessage // every field by ID, for custom fields
	Names    map[string]string          // field ID → display name
	Schema   map[string]FieldSchema
	EditMeta map[string]EditField
}

// FieldSchema is a field's declared type.
type FieldSchema struct {
	Type   string `json:"type"`
	Items  string `json:"items"`
	Custom string `json:"custom"`
}

// Issue fetches one issue with its field names and edit metadata.
func (c *Client) Issue(ctx context.Context, key string) (*Issue, error) {
	var v struct {
		ID       string                     `json:"id"`
		Key      string                     `json:"key"`
		Fields   map[string]json.RawMessage `json:"fields"`
		Names    map[string]string          `json:"names"`
		Schema   map[string]FieldSchema     `json:"schema"`
		EditMeta struct {
			Fields map[string]EditField `json:"fields"`
		} `json:"editmeta"`
	}
	q := url.Values{"expand": {"names,schema,editmeta"}, "fields": {"*all,-comment,-worklog"}}
	if err := c.do(ctx, "GET", "/rest/api/3/issue/"+esc(key)+"?"+q.Encode(), nil, &v); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v.Fields)
	if err != nil {
		return nil, err
	}
	is := &Issue{ID: v.ID, Key: v.Key, Raw: v.Fields, Names: v.Names, Schema: v.Schema, EditMeta: v.EditMeta.Fields}
	if err := json.Unmarshal(raw, &is.Fields); err != nil {
		return nil, fmt.Errorf("issue %s: %w", key, err)
	}
	return is, nil
}

// Description is the issue's description as markdown, "" when empty.
func (is *Issue) Description() string {
	doc, err := adf.Parse(is.Fields.Description)
	if err != nil || len(doc.Content) == 0 {
		return ""
	}
	return adf.ToMarkdown(doc)
}

// Editable reports whether editmeta lets Me change a field.
func (is *Issue) Editable(field string) bool {
	_, ok := is.EditMeta[field]
	return ok
}

// AllowedComponents are the components editmeta offers.
func (is *Issue) AllowedComponents() []Component {
	var out []Component
	for _, raw := range is.EditMeta["components"].AllowedValues {
		var c Component
		if json.Unmarshal(raw, &c) == nil && c.ID != "" {
			out = append(out, c)
		}
	}
	return out
}

// Component is a project component.
type Component struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CustomField is one non-empty custom field, ready to show.
type CustomField struct {
	ID, Name, Value string
}

// hiddenCustom are custom field types that are noise in a terminal.
var hiddenCustom = []string{
	"gh-lexo-rank",         // Rank
	"devsummarycf",         // Development panel
	"jsw-issue-color",      // epic color
	"service-entity-field", // JSM internals
	"vp-origin",            // JSM internals
	"sd-request-feedback",  // JSM internals
	"sd-customer-organizations",
}

// CustomFields lists the custom fields with a value, by display name.
func (is *Issue) CustomFields() []CustomField {
	var out []CustomField
	for id, raw := range is.Raw {
		if !strings.HasPrefix(id, "customfield_") {
			continue
		}
		if s := is.Schema[id]; s.Custom != "" && hidden(s.Custom) {
			continue
		}
		v := FieldText(raw)
		if v == "" {
			continue
		}
		out = append(out, CustomField{ID: id, Name: cmpOr(is.Names[id], id), Value: v})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func hidden(custom string) bool {
	for _, h := range hiddenCustom {
		if strings.HasSuffix(custom, h) {
			return true
		}
	}
	return false
}

// FieldText renders any field value as one line: a user's name, an
// option's value, a sprint's name, rich text's words, a list joined by
// commas. Empty values give "".
func FieldText(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return valueText(v)
}

func valueText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		if v == "{}" {
			return ""
		}
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "yes"
		}
		return "no"
	case []any:
		var parts []string
		for _, x := range v {
			if s := valueText(x); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		if v["type"] == "doc" {
			b, _ := json.Marshal(v)
			doc, err := adf.Parse(b)
			if err != nil {
				return ""
			}
			return adf.PlainText(doc)
		}
		for _, k := range []string{"displayName", "value", "name", "key", "title"} {
			if s, ok := v[k].(string); ok && s != "" {
				if child, ok := v["child"].(map[string]any); ok {
					if c := valueText(child); c != "" {
						return s + " › " + c
					}
				}
				return s
			}
		}
	}
	return ""
}

// Time parses Jira's timestamps, such as 2026-10-14T02:03:22.153-0500.
func Time(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.000-0700", time.RFC3339Nano, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

var (
	keyPattern  = regexp.MustCompile(`(?i)\b([A-Z][A-Z0-9_]+-[0-9]+)\b`)
	onlyNumber  = regexp.MustCompile(`^#?([0-9]+)$`)
	projectPart = regexp.MustCompile(`^([A-Z][A-Z0-9_]+)-`)
)

// ParseKey reads what was typed in Go to issue: a key, a bare number in
// project (the last one used), or a link with a key in it.
func ParseKey(input, project string) (string, bool) {
	s := strings.TrimSpace(input)
	if m := onlyNumber.FindStringSubmatch(s); m != nil {
		if project == "" {
			return "", false
		}
		return project + "-" + m[1], true
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		if k := u.Query().Get("selectedIssue"); k != "" {
			return strings.ToUpper(k), true
		}
		if i := strings.Index(u.Path, "/browse/"); i >= 0 {
			s = u.Path[i+len("/browse/"):]
		}
	}
	if m := keyPattern.FindStringSubmatch(s); m != nil {
		return strings.ToUpper(m[1]), true
	}
	return "", false
}

// ProjectOf is the project part of a key: "DEMO" of "DEMO-42".
func ProjectOf(key string) string {
	if m := projectPart.FindStringSubmatch(key); m != nil {
		return m[1]
	}
	return ""
}

// BranchName is a git branch name for an issue: KEY-summary-in-kebab.
func BranchName(key, summary string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(summary) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 50 { // cut at a word end
		slug = slug[:50]
		if i := strings.LastIndexByte(slug, '-'); i > 0 {
			slug = slug[:i]
		}
	}
	if slug == "" {
		return key
	}
	return key + "-" + slug
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
