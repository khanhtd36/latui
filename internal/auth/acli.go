package auth

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zalando/go-keyring"
)

// Acli reuses the OAuth token Atlassian's acli keeps after
// `acli jira auth login --web`: its profile in jira_config.yaml says which
// site and user, the OS secure store holds the token (see ADR 0003).
type Acli struct {
	// ConfigDir overrides ~/.config/acli, for tests.
	ConfigDir string
	// Renew makes acli refresh an expired token; nil runs acli itself.
	Renew func(ctx context.Context) error
	// Secret reads the stored token; nil reads the OS secure store.
	Secret func(service, user string) (string, error)
}

type acliProfile struct {
	Site, CloudID, AccountID, DisplayName, Email, AuthType string
}

type acliToken struct {
	AccessToken string    `json:"access_token"`
	Expiry      time.Time `json:"expiry"`
}

func (a Acli) Credential(ctx context.Context) (Credential, error) {
	p, err := a.profile()
	if err != nil {
		return Credential{}, err
	}
	if p.AuthType != "" && p.AuthType != "oauth" {
		return Credential{}, fmt.Errorf("acli is signed in with %s, latui needs 'acli jira auth login --web' (or set LATUI_EMAIL and LATUI_API_TOKEN)", p.AuthType)
	}
	cred, err := a.read(p)
	if err != nil {
		return Credential{}, err
	}
	if !cred.expired(time.Now()) {
		return cred, nil
	}
	renew := a.Renew
	if renew == nil {
		renew = renewWithAcli
	}
	if err := renew(ctx); err != nil {
		return Credential{}, fmt.Errorf("%w (%w)", ErrExpired, err)
	}
	if cred, err = a.read(p); err != nil {
		return Credential{}, err
	}
	if cred.expired(time.Now()) {
		return Credential{}, ErrExpired
	}
	return cred, nil
}

func (a Acli) read(p acliProfile) (Credential, error) {
	secret := a.Secret
	if secret == nil {
		secret = keyring.Get
	}
	raw, err := secret("acli:jira:"+p.CloudID, p.AccountID)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return Credential{}, errors.New("no acli token found: run 'acli jira auth login --web'")
		}
		return Credential{}, fmt.Errorf("reading acli's token: %w", err)
	}
	tok, err := decodeToken([]byte(raw))
	if err != nil {
		return Credential{}, err
	}
	return Credential{
		Site: p.Site, CloudID: p.CloudID, AccountID: p.AccountID,
		Email: p.Email, Name: p.DisplayName,
		bearer: tok.AccessToken, Expiry: tok.Expiry,
	}, nil
}

// decodeToken reads acli's stored token: JSON, gzip-compressed.
func decodeToken(b []byte) (acliToken, error) {
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return acliToken{}, fmt.Errorf("acli token: %w", err)
		}
		if b, err = io.ReadAll(zr); err != nil {
			return acliToken{}, fmt.Errorf("acli token: %w", err)
		}
	}
	var t acliToken
	if err := json.Unmarshal(b, &t); err != nil {
		return acliToken{}, fmt.Errorf("acli token is not in a format latui knows: %w", err)
	}
	if t.AccessToken == "" {
		return acliToken{}, errors.New("acli token has no access token")
	}
	return t, nil
}

func (a Acli) profile() (acliProfile, error) {
	dir := a.ConfigDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return acliProfile{}, err
		}
		dir = filepath.Join(home, ".config", "acli")
	}
	b, err := os.ReadFile(filepath.Join(dir, "jira_config.yaml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return acliProfile{}, errors.New("acli is not signed in: run 'acli jira auth login --web' (or set LATUI_EMAIL and LATUI_API_TOKEN)")
		}
		return acliProfile{}, err
	}
	return parseProfile(b)
}

// parseProfile reads the current profile from acli's jira_config.yaml,
// a flat file of a few keys, without a YAML library.
func parseProfile(b []byte) (acliProfile, error) {
	var current string
	var profiles []acliProfile
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		item := strings.HasPrefix(line, "- ")
		line = strings.TrimPrefix(line, "- ")
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "current_profile" {
			current = value
			continue
		}
		if item {
			profiles = append(profiles, acliProfile{})
		}
		if len(profiles) == 0 {
			continue
		}
		p := &profiles[len(profiles)-1]
		switch key {
		case "site":
			p.Site = value
		case "cloud_id":
			p.CloudID = value
		case "account_id":
			p.AccountID = value
		case "display_name":
			p.DisplayName = value
		case "email":
			p.Email = value
		case "auth_type":
			p.AuthType = value
		}
	}
	for _, p := range profiles {
		if current == "" || current == p.CloudID+":"+p.AccountID {
			if p.CloudID == "" || p.AccountID == "" {
				break
			}
			return p, nil
		}
	}
	return acliProfile{}, errors.New("acli has no current Jira profile: run 'acli jira auth login --web'")
}

// renewWithAcli runs an acli command that calls Jira, which makes acli
// refresh and store its token.
func renewWithAcli(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "acli", "jira", "auth", "status").CombinedOutput()
	if err != nil {
		return fmt.Errorf("acli jira auth status: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
