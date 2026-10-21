package teams

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/khanhtd36/latui/internal/auth"
)

type site struct{}

func (site) Credential(context.Context) (auth.Credential, error) {
	return auth.Credential{Site: "x.atlassian.net", CloudID: "cloud-1"}, nil
}

func gateway(t *testing.T, answers map[string]string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Basic ") {
			t.Errorf("not signed in with the token: %q", r.Header.Get("Authorization"))
		}
		var req struct {
			Op   string         `json:"operationName"`
			Vars map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Op == "TeamsSearch" && req.Vars["org"] != "ari:cloud:platform::org/org-1" {
			_, _ = w.Write([]byte(`{"errors":[{"message":"bad organization id"}]}`))
			return
		}
		_, _ = w.Write([]byte(answers[req.Op]))
	}))
	t.Cleanup(srv.Close)
	return &Client{Site: site{}, Email: "me@x.com", Token: "t", HTTP: srv.Client(), URL: srv.URL}
}

func TestSearchFindsTeams(t *testing.T) {
	c := gateway(t, map[string]string{
		"TeamsOrg":    `{"data":{"tenantContexts":[{"orgId":"org-1"}]}}`,
		"TeamsSearch": `{"data":{"team":{"teamSearchV2":{"nodes":[{"memberCount":3,"team":{"id":"ari:cloud:identity::team/u-1","displayName":"MITS11 Testers"}}]}}},"errors":[{"message":"Warning : You MUST provide a query operation name."}]}`,
	})
	got, err := c.Search(context.Background(), "MITS11")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "u-1" || got[0].Name != "MITS11 Testers" || got[0].Members != 3 {
		t.Errorf("teams %+v", got)
	}
}

func TestMembers(t *testing.T) {
	c := gateway(t, map[string]string{
		"TeamMembers": `{"data":{"team":{"teamV2":{"members":{"nodes":[{"member":{"accountId":"a","name":"Ann"}},{"member":{"accountId":"b","name":"Bob"}}]}}}}}`,
	})
	got, err := c.Members(context.Background(), "u-1")
	if err != nil || len(got) != 2 || got[0] != (Member{"a", "Ann"}) || got[1].Name != "Bob" {
		t.Errorf("members %+v, %v", got, err)
	}
}

func TestErrorsComeBack(t *testing.T) {
	c := gateway(t, map[string]string{"TeamMembers": `{"errors":[{"message":"no scope"}],"data":{"team":{"teamV2":null}}}`})
	if _, err := c.Members(context.Background(), "u-1"); err == nil || !strings.Contains(err.Error(), "no scope") {
		t.Errorf("error %v", err)
	}
}

func TestFromEnvNeedsBoth(t *testing.T) {
	env := map[string]string{"LATUI_TEAMS_EMAIL": "a@b.c"}
	if FromEnv(site{}, func(k string) string { return env[k] }).Available() {
		t.Error("teams should be off without a token")
	}
	env["LATUI_TEAMS_TOKEN"] = "x"
	if !FromEnv(site{}, func(k string) string { return env[k] }).Available() {
		t.Error("teams should be on with both")
	}
}
