package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

// oauthEndpoints is the slice of OIDC discovery this package needs; the
// endpoints always come from the issuer, never from config.
type oauthEndpoints struct {
	DeviceAuthURL string `json:"device_authorization_endpoint"`
	TokenURL      string `json:"token_endpoint"`
}

// discover fetches the issuer's OIDC discovery document.
func discover(ctx context.Context, issuer string) (oauthEndpoints, error) {
	var eps oauthEndpoints
	url := issuer + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return eps, fmt.Errorf("discovery: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return eps, fmt.Errorf("discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return eps, fmt.Errorf("discovery: %s returned %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&eps); err != nil {
		return eps, fmt.Errorf("discovery: parse %s: %w", url, err)
	}
	if eps.TokenURL == "" {
		return eps, fmt.Errorf("discovery: %s names no token_endpoint", url)
	}
	return eps, nil
}

func (oc *OAuthConfig) oauth2Config(eps oauthEndpoints) *oauth2.Config {
	return &oauth2.Config{
		ClientID: oc.ClientID,
		Scopes:   oc.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: eps.DeviceAuthURL, DeviceAuthURL: eps.DeviceAuthURL, TokenURL: eps.TokenURL},
	}
}

// deviceLogin runs the RFC 8628 device authorization grant: print the
// verification URL and code on out, poll the token endpoint at the server's
// interval, return the token pair.
func deviceLogin(ctx context.Context, oc *OAuthConfig, out io.Writer) (*oauth2.Token, error) {
	eps, err := discover(ctx, oc.Issuer)
	if err != nil {
		return nil, err
	}
	if eps.DeviceAuthURL == "" {
		return nil, fmt.Errorf("issuer %s advertises no device_authorization_endpoint", oc.Issuer)
	}
	cfg := oc.oauth2Config(eps)
	da, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, fmt.Errorf("device authorization: %w", err)
	}
	if da.VerificationURIComplete != "" {
		fmt.Fprintf(out, "Open %s\n", da.VerificationURIComplete)
		fmt.Fprintf(out, "(or go to %s and enter code %s)\n", da.VerificationURI, da.UserCode)
	} else {
		fmt.Fprintf(out, "Go to %s and enter code %s\n", da.VerificationURI, da.UserCode)
	}
	tok, err := cfg.DeviceAccessToken(ctx, da)
	if err != nil {
		return nil, fmt.Errorf("device authorization: %w", err)
	}
	return tok, nil
}

// refreshToken spends one refresh token at the issuer's token endpoint and
// returns the new pair. Callers hold the cache lock (tokens and refresh,
// idp-auth design): rotation makes a concurrent spend a grant-revoking
// hazard.
func refreshToken(ctx context.Context, oc *OAuthConfig, refresh string) (*oauth2.Token, error) {
	eps, err := discover(ctx, oc.Issuer)
	if err != nil {
		return nil, err
	}
	cfg := oc.oauth2Config(eps)
	tok, err := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	return tok, nil
}
