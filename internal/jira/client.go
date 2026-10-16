// Package jira is the Jira Cloud REST v3 client: issues, comments,
// history, transitions and the edits latui makes.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/khanhtd36/latui/internal/auth"
)

// Client calls Jira for the credential's site.
type Client struct {
	Creds auth.Source
	HTTP  *http.Client
	// Base overrides the credential's REST base URL, for tests.
	Base string
}

func New(creds auth.Source) *Client {
	return &Client{Creds: creds, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// Site is the signed-in credential, for links and the title line.
func (c *Client) Site(ctx context.Context) (auth.Credential, error) {
	return c.Creds.Credential(ctx)
}

// APIError is a non-2xx answer, with Jira's own messages when it sent any.
type APIError struct {
	Status   int
	Messages []string
}

func (e *APIError) Error() string {
	if len(e.Messages) == 0 {
		return fmt.Sprintf("jira: HTTP %d", e.Status)
	}
	return "jira: " + strings.Join(e.Messages, "; ")
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	cred, err := c.Creds.Credential(ctx)
	if err != nil {
		return err
	}
	base := c.Base
	if base == "" {
		base = cred.JiraBase()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", cred.Header())
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return apiError(resp.StatusCode, b)
	}
	if out == nil || len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	return json.Unmarshal(b, out)
}

func apiError(status int, b []byte) error {
	var v struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
		Message       string            `json:"message"`
	}
	e := &APIError{Status: status}
	if json.Unmarshal(b, &v) == nil {
		e.Messages = append(e.Messages, v.ErrorMessages...)
		for field, msg := range v.Errors {
			e.Messages = append(e.Messages, field+": "+msg)
		}
		if v.Message != "" {
			e.Messages = append(e.Messages, v.Message)
		}
	}
	if len(e.Messages) == 0 {
		switch status {
		case http.StatusUnauthorized:
			e.Messages = []string{"not signed in (401): run 'acli jira auth login --web'"}
		case http.StatusForbidden:
			e.Messages = []string{"not allowed (403)"}
		case http.StatusNotFound:
			e.Messages = []string{"not found, or no permission to see it (404)"}
		}
	}
	return e
}

func esc(s string) string { return url.PathEscape(s) }
