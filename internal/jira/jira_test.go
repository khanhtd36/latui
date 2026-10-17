package jira

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/khanhtd36/latui/internal/auth"
)

type fakeCreds struct{}

func (fakeCreds) Credential(context.Context) (auth.Credential, error) {
	return auth.Credential{Site: "x.atlassian.net", CloudID: "c"}, nil
}

// serve answers each "METHOD path" with a body and records request bodies.
func serve(t *testing.T, routes map[string]string) (*Client, map[string]string) {
	t.Helper()
	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.Path
		got[key] = string(b)
		body, ok := routes[key]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errorMessages":["Issue does not exist"]}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{Creds: fakeCreds{}, HTTP: srv.Client(), Base: srv.URL}, got
}

func TestIssueFields(t *testing.T) {
	c, _ := serve(t, map[string]string{"GET /rest/api/3/issue/QA-1": `{
		"id":"1","key":"QA-1",
		"names":{"customfield_1":"Team","customfield_2":"Rank","customfield_3":"Story points"},
		"schema":{"customfield_2":{"custom":"com.pyxis.greenhopper.jira:gh-lexo-rank"}},
		"editmeta":{"fields":{"labels":{},"components":{"allowedValues":[{"id":"10","name":"UI"}]}}},
		"fields":{"summary":"Fix it","status":{"name":"In Progress","statusCategory":{"key":"indeterminate"}},
			"labels":["a"],"assignee":{"accountId":"u1","displayName":"Me"},
			"description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Body"}]}]},
			"customfield_1":{"value":"Platform"},"customfield_2":"0|i04uq0:","customfield_3":5,"customfield_4":null}}`})
	is, err := c.Issue(context.Background(), "QA-1")
	if err != nil {
		t.Fatal(err)
	}
	if is.Fields.Summary != "Fix it" || is.Fields.Assignee.DisplayName != "Me" || is.Description() != "Body\n" {
		t.Errorf("fields %+v", is.Fields)
	}
	if !is.Editable("labels") || is.Editable("assignee") || len(is.AllowedComponents()) != 1 {
		t.Errorf("editmeta %+v", is.EditMeta)
	}
	cf := is.CustomFields()
	if len(cf) != 2 || cf[0].Name != "Story points" || cf[0].Value != "5" || cf[1].Value != "Platform" {
		t.Errorf("custom fields %+v", cf)
	}
}

func TestNotFoundError(t *testing.T) {
	c, _ := serve(t, nil)
	_, err := c.Issue(context.Background(), "NOPE-1")
	if err == nil || !strings.Contains(err.Error(), "Issue does not exist") {
		t.Fatalf("got %v", err)
	}
}

func TestTransitionsResolution(t *testing.T) {
	c, got := serve(t, map[string]string{
		"GET /rest/api/3/issue/QA-1/transitions": `{"transitions":[
			{"id":"31","name":"Done","to":{"name":"Done"},"fields":{"resolution":{"required":true,"name":"Resolution","allowedValues":[{"name":"Fixed"},{"name":"Won't Do"}]}}},
			{"id":"41","name":"Review","to":{"name":"Review"},"fields":{"customfield_9":{"required":true,"name":"Reviewer"}}}]}`,
		"POST /rest/api/3/issue/QA-1/transitions": ``,
	})
	ts, err := c.Transitions(context.Background(), "QA-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ts[0].NeedsResolution || len(ts[0].Resolutions) != 2 || len(ts[1].OtherRequired) != 1 {
		t.Errorf("transitions %+v", ts)
	}
	if err := c.Transition(context.Background(), "QA-1", "31", "Fixed"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got["POST /rest/api/3/issue/QA-1/transitions"], `"resolution":{"name":"Fixed"}`) {
		t.Errorf("body %s", got["POST /rest/api/3/issue/QA-1/transitions"])
	}
}

func TestEdits(t *testing.T) {
	c, got := serve(t, map[string]string{
		"PUT /rest/api/3/issue/QA-1":          ``,
		"PUT /rest/api/3/issue/QA-1/assignee": ``,
		"POST /rest/api/3/issue/QA-1/comment": `{}`,
	})
	ctx := context.Background()
	if err := c.SetLabels(ctx, "QA-1", nil); err != nil {
		t.Fatal(err)
	}
	if got["PUT /rest/api/3/issue/QA-1"] != `{"fields":{"labels":[]}}` {
		t.Errorf("labels body %s", got["PUT /rest/api/3/issue/QA-1"])
	}
	if err := c.Assign(ctx, "QA-1", ""); err != nil {
		t.Fatal(err)
	}
	if got["PUT /rest/api/3/issue/QA-1/assignee"] != `{"accountId":null}` {
		t.Errorf("unassign body %s", got["PUT /rest/api/3/issue/QA-1/assignee"])
	}
	if err := c.AddComment(ctx, "QA-1", "hi **there**"); err != nil {
		t.Fatal(err)
	}
	var body struct {
		Body struct {
			Type string `json:"type"`
		} `json:"body"`
	}
	if err := json.Unmarshal([]byte(got["POST /rest/api/3/issue/QA-1/comment"]), &body); err != nil || body.Body.Type != "doc" {
		t.Errorf("comment body %s", got["POST /rest/api/3/issue/QA-1/comment"])
	}
}

func TestHistoryNewestFirst(t *testing.T) {
	c, _ := serve(t, map[string]string{"GET /rest/api/3/issue/QA-1/changelog": `{"total":2,"isLast":true,"values":[
		{"author":{"displayName":"A"},"created":"2026-10-14T02:03:22.153-0500","items":[{"field":"status","fromString":"To Do","toString":"In Progress"}]},
		{"author":{"displayName":"B"},"created":"2026-10-15T02:03:22.153-0500","items":[{"field":"labels","fromString":"","toString":"x"}]}]}`})
	h, err := c.History(context.Background(), "QA-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 || h[0].Author.DisplayName != "B" {
		t.Errorf("history %+v", h)
	}
}

func TestParseKey(t *testing.T) {
	for _, tc := range []struct{ in, project, want string }{
		{"DEMO-42", "", "DEMO-42"},
		{"demo-42", "", "DEMO-42"},
		{"42", "DEMO", "DEMO-42"},
		{"https://x.atlassian.net/browse/QA-17?focusedCommentId=1", "", "QA-17"},
		{"https://x.atlassian.net/jira/software/projects/QA/boards/1?selectedIssue=QA-7", "", "QA-7"},
	} {
		if got, ok := ParseKey(tc.in, tc.project); !ok || got != tc.want {
			t.Errorf("ParseKey(%q, %q) = %q %v, want %q", tc.in, tc.project, got, ok, tc.want)
		}
	}
	if _, ok := ParseKey("3499", ""); ok {
		t.Error("a bare number with no project should not parse")
	}
}

func TestBranchName(t *testing.T) {
	if got := BranchName("DEMO-42", "Stream large exports instead of buffering them"); got != "DEMO-42-stream-large-exports-instead-of-buffering-them" {
		t.Errorf("got %q", got)
	}
}

func TestSearchRowsWithMissingFields(t *testing.T) {
	c, got := serve(t, map[string]string{"POST /rest/api/3/search/jql": `{"isLast":true,"issues":[
		{"key":"QA-1","fields":{"summary":"S","status":{"name":"Deferred","statusCategory":{"key":"new"}},"issuetype":{"name":"Bug"},"priority":{"name":"High"},"parent":{"key":"QA-0"},"updated":"2026-10-15T02:03:22.153-0500"}},
		{"key":"DEMO-2"}]}`})
	rows, err := c.Search(context.Background(), "assignee = currentUser()")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Status.Name != "Deferred" || rows[0].Parent != "QA-0" || rows[0].Priority != "High" || rows[1].Project != "DEMO" {
		t.Errorf("rows %+v", rows)
	}
	if !strings.Contains(got["POST /rest/api/3/search/jql"], `"jql":"assignee = currentUser()"`) {
		t.Errorf("body %s", got["POST /rest/api/3/search/jql"])
	}
}

func TestCreateIssueBody(t *testing.T) {
	c, got := serve(t, map[string]string{"POST /rest/api/3/issue": `{"key":"QA-9"}`})
	key, err := c.CreateIssue(context.Background(), NewIssue{
		Project: "QA", TypeID: "1", Summary: "Crash", Description: "It **crashes**",
		Labels: []string{"x"}, Components: []string{"10"}, PriorityID: "2", Parent: "QA-1",
		Users: map[string][]string{"customfield_1": {"u1"}},
	})
	if err != nil || key != "QA-9" {
		t.Fatalf("key %q err %v", key, err)
	}
	body := got["POST /rest/api/3/issue"]
	for _, want := range []string{`"project":{"key":"QA"}`, `"issuetype":{"id":"1"}`, `"components":[{"id":"10"}]`, `"parent":{"key":"QA-1"}`, `"customfield_1":[{"accountId":"u1"}]`, `"type":"doc"`} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"assignee"`) {
		t.Errorf("Automatic should send no assignee: %s", body)
	}
}
