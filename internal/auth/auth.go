// Package auth finds the credential latui signs in with: acli's OAuth
// token by default, or an API token from the environment.
package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrExpired is a session that acli could not renew: sign in again.
var ErrExpired = errors.New("session expired: run 'acli jira auth login --web'")

// Credential is one signed-in user on one Site, ready to call its APIs.
type Credential struct {
	Site      string // host, such as example.atlassian.net
	CloudID   string
	AccountID string
	Email     string
	Name      string

	bearer string    // an OAuth access token
	basic  string    // email:token
	Expiry time.Time // zero for an API token
}

// Header is the Authorization header value.
func (c Credential) Header() string {
	if c.bearer != "" {
		return "Bearer " + c.bearer
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.basic))
}

// JiraBase is where Jira REST paths (/rest/api/3/...) hang: OAuth tokens
// go through the api.atlassian.com gateway, API tokens to the site itself.
func (c Credential) JiraBase() string {
	if c.bearer != "" {
		return "https://api.atlassian.com/ex/jira/" + c.CloudID
	}
	return "https://" + c.Site
}

// GraphQLURL is the Atlassian GraphQL gateway for this credential.
func (c Credential) GraphQLURL() string {
	if c.bearer != "" {
		return "https://api.atlassian.com/graphql"
	}
	return "https://" + c.Site + "/gateway/api/graphql"
}

// SiteURL is the site's web address, for links opened in the browser.
func (c Credential) SiteURL() string { return "https://" + c.Site }

// SiteARI scopes notification queries to this site: without it the feed
// comes back empty and marking fails.
func (c Credential) SiteARI() string { return "ari:cloud:platform::site/" + c.CloudID }

func (c Credential) expired(now time.Time) bool {
	return !c.Expiry.IsZero() && now.After(c.Expiry.Add(-time.Minute))
}

// Source hands out a valid credential, renewing it when it can.
type Source interface {
	Credential(ctx context.Context) (Credential, error)
}

// Cached keeps a Source's credential until shortly before it expires.
type Cached struct {
	Source Source
	mu     sync.Mutex
	cred   *Credential
}

func (c *Cached) Credential(ctx context.Context) (Credential, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cred != nil && !c.cred.expired(time.Now()) {
		return *c.cred, nil
	}
	cred, err := c.Source.Credential(ctx)
	if err != nil {
		return Credential{}, err
	}
	c.cred = &cred
	return cred, nil
}

// Default is the environment's API token when LATUI_EMAIL and
// LATUI_API_TOKEN are set, else acli's login.
func Default(getenv func(string) string) Source {
	email, token := getenv("LATUI_EMAIL"), getenv("LATUI_API_TOKEN")
	if email != "" && token != "" {
		return &Cached{Source: Env{Site: siteHost(getenv("LATUI_SITE")), Email: email, Token: token}}
	}
	return &Cached{Source: Acli{}}
}

// siteHost turns "https://example.atlassian.net/" or "example" into
// "example.atlassian.net".
func siteHost(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	s = strings.TrimRight(strings.SplitN(s, "/", 2)[0], "/")
	if s != "" && !strings.Contains(s, ".") {
		s += ".atlassian.net"
	}
	return s
}
