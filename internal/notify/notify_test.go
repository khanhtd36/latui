package notify

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
	return auth.Credential{Site: "x.atlassian.net", CloudID: "cloud-1"}, nil
}

const feedJSON = `{"data":{"notifications":{"notificationFeed":{
 "pageInfo":{"hasNextPage":true,"endCursor":"c2"},
 "nodes":[
  {"groupId":"g1","groupSize":2,"readStates":["unread"],"headNotification":{
    "notificationId":"n1","timestamp":"2026-10-16T10:36:18.725Z","readState":"read","category":"direct",
    "analyticsAttributes":[{"key":"registrationProduct","value":"jira"},{"key":"messageTemplateId","value":"jira-issue-comment"}],
    "content":{"type":"created","message":"Sam Rivera commented on a bug",
      "url":"https://x.atlassian.net/browse/QA-17?focusedCommentId=1#comment-1",
      "actor":{"displayName":"Sam Rivera"},
      "entity":{"title":"Export fails on large files","url":"https://x.atlassian.net/browse/QA-17","status":"In Progress"},
      "path":[{"title":"QA-17","url":"https://x.atlassian.net/browse/QA-17"}],
      "bodyItems":[{"type":"PRIMARY","document":{"format":"ADF","data":"{\"type\":\"doc\",\"content\":[{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"Looks good\"}]}]}"}}]}}},
  {"groupId":"g2","groupSize":1,"readStates":["read"],"headNotification":{
    "notificationId":"n2","timestamp":"2026-10-16T08:50:42.926Z","readState":"read","category":"direct",
    "analyticsAttributes":[{"key":"registrationProduct","value":"confluence"}],
    "content":{"type":"mentioned","message":"Priya mentioned you on a page",
      "url":"https://x.atlassian.net/wiki/spaces/QA/pages/1/Schedule",
      "actor":{"displayName":"Priya"},
      "entity":{"title":"Schedule","url":"https://x.atlassian.net/wiki/spaces/QA/pages/1/Schedule","status":null},
      "path":[{"title":"Quality Assurance","url":"https://x.atlassian.net/wiki/spaces/QA"}],
      "bodyItems":[]}}}
 ]}}}}`

func TestFeedDecodes(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		_, _ = w.Write([]byte(feedJSON))
	}))
	defer srv.Close()
	c := &Client{Creds: fakeCreds{}, HTTP: srv.Client(), URL: srv.URL}

	page, err := c.Feed(context.Background(), Direct, true, 50, "c1")
	if err != nil {
		t.Fatal(err)
	}
	vars := got["variables"].(map[string]any)
	if vars["ari"] != "ari:cloud:platform::site/cloud-1" || vars["after"] != "c1" {
		t.Errorf("variables %v", vars)
	}
	if f := vars["filter"].(map[string]any); f["readStateFilter"] != "unread" || f["categoryFilter"] != "direct" {
		t.Errorf("filter %v", f)
	}
	if !page.HasMore || page.After != "c2" || len(page.Groups) != 2 {
		t.Fatalf("page %+v", page)
	}
	g := page.Groups[0]
	if !g.Unread || g.Size != 2 || g.Head.Product != Jira || g.Head.Key != "QA-17" || g.Head.Snippet != "Looks good" || g.Head.Status != "In Progress" {
		t.Errorf("jira group %+v", g)
	}
	h := page.Groups[1].Head
	if page.Groups[1].Unread || h.Product != Confluence || h.Context != "Quality Assurance" || h.Key != "" {
		t.Errorf("confluence group %+v", page.Groups[1])
	}
}

func TestSchemaErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Field 'notificationFeed' is undefined"}]}`))
	}))
	defer srv.Close()
	c := &Client{Creds: fakeCreds{}, HTTP: srv.Client(), URL: srv.URL}
	_, err := c.Feed(context.Background(), Watching, false, 50, "")
	var se *SchemaError
	if err == nil || !strings.Contains(err.Error(), "undefined") {
		t.Fatalf("got %v", err)
	}
	if !asSchema(err, &se) {
		t.Errorf("not a SchemaError: %T", err)
	}
}

func asSchema(err error, se **SchemaError) bool {
	e, ok := err.(*SchemaError) //nolint:errorlint // returned unwrapped
	*se = e
	return ok
}

func TestKeyFromURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://x.atlassian.net/browse/DEMO-7?actionerId=1":                              "DEMO-7",
		"https://x.atlassian.net/jira/software/c/projects/QA/boards/1?selectedIssue=QA-7": "QA-7",
		"https://x.atlassian.net/wiki/spaces/QA":                                          "",
	} {
		if got := KeyFromURL(in); got != want {
			t.Errorf("KeyFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}
