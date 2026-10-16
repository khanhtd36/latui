package auth

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const config = `version: 1
current_profile: cloud-2:acct-2
profiles:
    - site: one.atlassian.net
      cloud_id: cloud-1
      account_id: acct-1
      auth_type: oauth
    - site: two.atlassian.net
      cloud_id: cloud-2
      account_id: acct-2
      display_name: Me Two
      email: me@two.example
      auth_type: oauth
`

func TestParseProfilePicksCurrent(t *testing.T) {
	p, err := parseProfile([]byte(config))
	if err != nil {
		t.Fatal(err)
	}
	if p.Site != "two.atlassian.net" || p.CloudID != "cloud-2" || p.AccountID != "acct-2" || p.Email != "me@two.example" {
		t.Fatalf("got %+v", p)
	}
}

func gz(s string) string {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.String()
}

func acliIn(t *testing.T, secret func(service, user string) (string, error), renew func(context.Context) error) Acli {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "jira_config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return Acli{ConfigDir: dir, Secret: secret, Renew: renew}
}

func TestAcliReadsGzipToken(t *testing.T) {
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	var asked string
	a := acliIn(t, func(service, user string) (string, error) {
		asked = service + ":" + user
		return gz(`{"access_token":"tok","expiry":"` + exp + `"}`), nil
	}, nil)
	c, err := a.Credential(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if asked != "acli:jira:cloud-2:acct-2" {
		t.Errorf("asked the store for %q", asked)
	}
	if c.Header() != "Bearer tok" || c.JiraBase() != "https://api.atlassian.com/ex/jira/cloud-2" {
		t.Errorf("got %q %q", c.Header(), c.JiraBase())
	}
}

func TestAcliRenewsExpiredToken(t *testing.T) {
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	fresh := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	stored := old
	renewed := 0
	a := acliIn(t, func(string, string) (string, error) {
		return `{"access_token":"tok","expiry":"` + stored + `"}`, nil
	}, func(context.Context) error {
		renewed++
		stored = fresh
		return nil
	})
	if _, err := a.Credential(context.Background()); err != nil {
		t.Fatal(err)
	}
	if renewed != 1 {
		t.Fatalf("renewed %d times", renewed)
	}
}

func TestAcliStillExpiredAfterRenew(t *testing.T) {
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	a := acliIn(t, func(string, string) (string, error) {
		return `{"access_token":"tok","expiry":"` + old + `"}`, nil
	}, func(context.Context) error { return nil })
	if _, err := a.Credential(context.Background()); !errors.Is(err, ErrExpired) {
		t.Fatalf("got %v, want ErrExpired", err)
	}
}

func TestSiteHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.atlassian.net/": "example.atlassian.net",
		"example":                        "example.atlassian.net",
		"jira.example.com/browse/X-1":    "jira.example.com",
	} {
		if got := siteHost(in); got != want {
			t.Errorf("siteHost(%q) = %q, want %q", in, got, want)
		}
	}
}
