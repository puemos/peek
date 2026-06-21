package cli

import (
	"encoding/json"
	"net/http"
	"strings"
)

type authDiscovery struct {
	Providers []struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"providers"`
	BrowserLogin  bool `json:"browser_login"`
	OAuthRequired bool `json:"oauth_required"`
}

func discoverAuth(host string) (authDiscovery, error) {
	var out authDiscovery
	resp, err := httpClient.Get(strings.TrimRight(host, "/") + "/api/auth/providers")
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return out, nil
	}
	if resp.StatusCode >= 400 {
		return out, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	if len(out.Providers) > 0 {
		out.BrowserLogin = true
	}
	return out, nil
}

func oauthAvailable(host string) (bool, error) {
	out, err := discoverAuth(host)
	if err != nil {
		return false, err
	}
	return out.BrowserLogin, nil
}
