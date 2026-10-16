package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Env signs in with an API token from LATUI_SITE, LATUI_EMAIL and
// LATUI_API_TOKEN, for when acli isn't there (and for live tests).
type Env struct {
	Site, Email, Token string
}

func (e Env) Credential(ctx context.Context) (Credential, error) {
	if e.Site == "" {
		return Credential{}, errors.New("LATUI_SITE is not set (such as example.atlassian.net)")
	}
	cloudID, err := tenantCloudID(ctx, e.Site)
	if err != nil {
		return Credential{}, err
	}
	return Credential{Site: e.Site, CloudID: cloudID, Email: e.Email, basic: e.Email + ":" + e.Token}, nil
}

// tenantCloudID asks a site for its cloud ID; the endpoint needs no auth.
func tenantCloudID(ctx context.Context, site string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+site+"/_edge/tenant_info", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: tenant info: %s", site, resp.Status)
	}
	var v struct {
		CloudID string `json:"cloudId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil || v.CloudID == "" {
		return "", fmt.Errorf("%s: no cloud ID in tenant info", site)
	}
	return v.CloudID, nil
}
