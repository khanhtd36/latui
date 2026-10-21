package teams

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/khanhtd36/latui/internal/auth"
)

type site struct{}

func (site) Credential(context.Context) (auth.Credential, error) {
	return auth.Credential{Site: "x.atlassian.net", CloudID: "cloud-1", Email: "me@x.com"}, nil
}

// memStore is a Store in memory.
type memStore struct {
	logins  map[string]Login
	saveErr error
}

func (m *memStore) Load(site string) (Login, bool, error) {
	l, ok := m.logins[site]
	return l, ok, nil
}

func (m *memStore) Save(site string, l Login) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	if m.logins == nil {
		m.logins = map[string]Login{}
	}
	m.logins[site] = l
	return nil
}

func (m *memStore) Delete(site string) error {
	delete(m.logins, site)
	return nil
}

type rig struct {
	*Client
	store *memStore
	env   map[string]string
	auth  []string // the Authorization headers the gateway saw
}

// gateway is a fake gateway answering by operation; a token other than
// "good" is turned down with a 401.
func gateway(t *testing.T, answers map[string]string) *rig {
	t.Helper()
	r := &rig{store: &memStore{}, env: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.auth = append(r.auth, req.Header.Get("Authorization"))
		if !strings.HasSuffix(decode(req.Header.Get("Authorization")), ":good") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Op   string         `json:"operationName"`
			Vars map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		if body.Op == "TeamsSearch" && body.Vars["org"] != "ari:cloud:platform::org/org-1" {
			_, _ = w.Write([]byte(`{"errors":[{"message":"bad organization id"}]}`))
			return
		}
		_, _ = w.Write([]byte(answers[body.Op]))
	}))
	t.Cleanup(srv.Close)
	r.Client = New(site{}, func(k string) string { return r.env[k] }, r.store)
	r.HTTP, r.URL = srv.Client(), srv.URL
	return r
}

func decode(h string) string {
	h = strings.TrimPrefix(h, "Basic ")
	b, _ := base64Decode(h)
	return string(b)
}

var orgAnswer = map[string]string{"TeamsOrg": `{"data":{"tenantContexts":[{"orgId":"org-1"}]}}`}

func TestSearchFindsTeamsWithTheEnvToken(t *testing.T) {
	answers := map[string]string{
		"TeamsOrg":    orgAnswer["TeamsOrg"],
		"TeamsSearch": `{"data":{"team":{"teamSearchV2":{"nodes":[{"memberCount":3,"team":{"id":"ari:cloud:identity::team/u-1","displayName":"MITS11 Testers"}}]}}},"errors":[{"message":"Warning : You MUST provide a query operation name."}]}`,
	}
	r := gateway(t, answers)
	r.env["LATUI_TEAMS_TOKEN"] = "good"
	got, err := r.Search(context.Background(), "MITS11")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "u-1" || got[0].Name != "MITS11 Testers" || got[0].Members != 3 {
		t.Errorf("teams %+v", got)
	}
	if want := "me@x.com:good"; decode(r.auth[0]) != want {
		t.Errorf("signed in as %q, want the sign-in's own email with the token", decode(r.auth[0]))
	}
}

func TestMembers(t *testing.T) {
	r := gateway(t, map[string]string{
		"TeamMembers": `{"data":{"team":{"teamV2":{"members":{"nodes":[{"member":{"accountId":"a","name":"Ann"}},{"member":{"accountId":"b","name":"Bob"}}]}}}}}`,
	})
	r.env["LATUI_TEAMS_TOKEN"] = "good"
	got, err := r.Members(context.Background(), "u-1")
	if err != nil || len(got) != 2 || got[0] != (Member{"a", "Ann"}) || got[1].Name != "Bob" {
		t.Errorf("members %+v, %v", got, err)
	}
}

func TestNoTokenMeansOffAndNoLookup(t *testing.T) {
	r := gateway(t, nil)
	if r.State(context.Background()) != Off {
		t.Error("teams should be off with no token")
	}
	if _, err := r.Search(context.Background(), "x"); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("search without a token: %v", err)
	}
	if len(r.auth) != 0 {
		t.Error("the gateway was called without a token")
	}
}

func TestLoginChecksThenStores(t *testing.T) {
	r := gateway(t, orgAnswer)
	ctx := context.Background()
	if err := r.Login(ctx, "", "wrong"); err == nil || !strings.Contains(err.Error(), "turned down") {
		t.Fatalf("a wrong token was accepted: %v", err)
	}
	if len(r.store.logins) != 0 || r.State(ctx) != Off {
		t.Fatalf("a wrong token was stored: %+v", r.store.logins)
	}
	if err := r.Login(ctx, "", " good \n"); err != nil {
		t.Fatal(err)
	}
	if l := r.store.logins["x.atlassian.net"]; l.Token != "good" || l.Email != "" {
		t.Errorf("stored %+v: the token is trimmed and the email left to the sign-in", l)
	}
	if r.State(ctx) != Ready {
		t.Error("a stored token should turn teams on")
	}
}

func TestStoredTokenIsLoadedByANewClient(t *testing.T) {
	r := gateway(t, orgAnswer)
	_ = r.store.Save("x.atlassian.net", Login{Email: "other@x.com", Token: "good"})
	if r.State(context.Background()) != Ready {
		t.Fatal("a stored token should be found")
	}
	if _, err := r.org(context.Background(), auth.Credential{Site: "x.atlassian.net", CloudID: "cloud-1"}, Login{Email: "other@x.com", Token: "good"}); err != nil {
		t.Fatal(err)
	}
	if decode(r.auth[0]) != "other@x.com:good" {
		t.Errorf("signed in as %q: the stored email should win over the sign-in's", decode(r.auth[0]))
	}
}

func TestEnvBeatsTheStoredToken(t *testing.T) {
	r := gateway(t, map[string]string{"TeamMembers": `{"data":{"team":{"teamV2":{"members":{"nodes":[]}}}}}`})
	_ = r.store.Save("x.atlassian.net", Login{Token: "stored-bad"})
	r.env["LATUI_TEAMS_TOKEN"], r.env["LATUI_TEAMS_EMAIL"] = "good", "env@x.com"
	if _, err := r.Members(context.Background(), "u-1"); err != nil {
		t.Fatal(err)
	}
	if decode(r.auth[0]) != "env@x.com:good" {
		t.Errorf("signed in as %q, want the environment's", decode(r.auth[0]))
	}
}

func TestRejectedTokenIsReportedUntilReplaced(t *testing.T) {
	r := gateway(t, orgAnswer)
	ctx := context.Background()
	_ = r.store.Save("x.atlassian.net", Login{Token: "revoked"})
	if _, err := r.Members(ctx, "u-1"); err == nil {
		t.Fatal("a revoked token worked")
	}
	if r.State(ctx) != Rejected {
		t.Error("a turned-down token should show as rejected")
	}
	if err := r.Login(ctx, "", "good"); err != nil {
		t.Fatal(err)
	}
	if r.State(ctx) != Ready {
		t.Error("a new token should clear the rejection")
	}
}

func TestLogoutRemovesTheStoredToken(t *testing.T) {
	r := gateway(t, orgAnswer)
	ctx := context.Background()
	if err := r.Login(ctx, "", "good"); err != nil {
		t.Fatal(err)
	}
	if err := r.Logout(ctx); err != nil || r.State(ctx) != Off || len(r.store.logins) != 0 {
		t.Errorf("after logout: state %v, stored %+v, %v", r.State(ctx), r.store.logins, err)
	}
}

func TestStoreFailureSaysToUseTheEnvironment(t *testing.T) {
	r := gateway(t, orgAnswer)
	r.store.saveErr = ErrNoSecureStore{errors.New("no keyring")}
	err := r.Login(context.Background(), "", "good")
	var no ErrNoSecureStore
	if !errors.As(err, &no) || !strings.Contains(err.Error(), "LATUI_TEAMS_TOKEN") {
		t.Errorf("error %v", err)
	}
}

func TestTokenNeverAppearsInErrors(t *testing.T) {
	r := gateway(t, orgAnswer)
	err := r.Login(context.Background(), "", "super-secret-token")
	if err == nil || strings.Contains(err.Error(), "super-secret-token") {
		t.Errorf("error %v", err)
	}
}

func TestKeyringStoreRoundTrip(t *testing.T) {
	data := map[string]string{}
	s := KeyringStore{
		Get: func(svc, user string) (string, error) {
			if v, ok := data[svc+"|"+user]; ok {
				return v, nil
			}
			return "", keyring.ErrNotFound
		},
		Set:    func(svc, user, pw string) error { data[svc+"|"+user] = pw; return nil },
		Remove: func(svc, user string) error { delete(data, svc+"|"+user); return nil },
	}
	if _, ok, err := s.Load("x"); ok || err != nil {
		t.Fatalf("empty store: %v %v", ok, err)
	}
	if err := s.Save("x", Login{Email: "a@b.c", Token: "t"}); err != nil {
		t.Fatal(err)
	}
	if l, ok, _ := s.Load("x"); !ok || l.Token != "t" || l.Email != "a@b.c" {
		t.Errorf("loaded %+v", l)
	}
	if err := s.Delete("x"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load("x"); ok {
		t.Error("deleted login still there")
	}
	if err := s.Delete("x"); err != nil {
		t.Errorf("deleting what isn't there should be fine: %v", err)
	}
}

func base64Decode(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
