package auth

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// oauthError is an error response of an authorization server (RFC 6749
// Section 5.2).
type oauthError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *oauthError) Error() string {
	if e.Description == "" {
		return "auth: " + e.Code
	}
	return "auth: " + e.Code + ": " + e.Description
}

// client is how the client identifies itself at the token endpoint.
type client struct {
	id, secret string
}

// clientFor returns the credentials of the client id: the configured
// secret goes with the configured client alone.
func (a *Authorizer) clientFor(id string) client {
	if id == a.cfg.ClientID {
		return client{id: id, secret: a.cfg.ClientSecret}
	}
	return client{id: id}
}

// requestToken posts form to the token endpoint and returns the token for
// resource.
func (a *Authorizer) requestToken(ctx context.Context, as *serverMetadata, c client, form url.Values) (*Token, error) {
	// RFC 8414 makes client_secret_basic the method of a server that names
	// none.
	basic := c.secret != "" && (len(as.TokenEndpointAuthMethods) == 0 || slices.Contains(as.TokenEndpointAuthMethods, "client_secret_basic"))
	if !basic {
		form.Set("client_id", c.id)
		if c.secret != "" {
			form.Set("client_secret", c.secret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, as.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basic {
		req.SetBasicAuth(url.QueryEscape(c.id), url.QueryEscape(c.secret))
	}
	var resp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		oauthError
	}
	if err := a.do(req, &resp); err != nil {
		return nil, err
	}
	// GitHub answers errors with 200.
	if resp.Code != "" {
		return nil, &resp.oauthError
	}
	if resp.AccessToken == "" {
		return nil, fmt.Errorf("auth: %s returned no access token", as.Issuer)
	}
	if !strings.EqualFold(resp.TokenType, "Bearer") {
		return nil, fmt.Errorf("auth: %s returned a %q token, not a bearer token", as.Issuer, resp.TokenType)
	}
	t := &Token{
		Issuer:       as.Issuer,
		ClientID:     c.id,
		Resource:     form.Get("resource"),
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		Scope:        resp.Scope,
	}
	if resp.ExpiresIn > 0 {
		t.Expiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	}
	return t, nil
}

// refresh trades t's refresh token for a new token and stores it. Caller
// holds a.mu.
func (a *Authorizer) refresh(ctx context.Context, t *Token) (*Token, error) {
	as, err := a.serverMetadata(ctx, t.Issuer)
	if err != nil {
		return nil, err
	}
	r, err := a.requestToken(ctx, as, a.clientFor(t.ClientID), url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.RefreshToken},
		"resource":      {t.Resource},
	})
	if err != nil {
		return nil, err
	}
	// A server that does not rotate refresh tokens returns none.
	r.RefreshToken = cmp.Or(r.RefreshToken, t.RefreshToken)
	r.Scope = cmp.Or(r.Scope, t.Scope)
	if err := a.cfg.Store.SaveToken(a.cfg.Server, r); err != nil {
		return nil, err
	}
	return r, nil
}

// do sends req and decodes a 2xx JSON response into v; an error response
// becomes an *oauthError when it is one.
func (a *Authorizer) do(req *http.Request, v any) error {
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		var oe oauthError
		if json.Unmarshal(data, &oe) == nil && oe.Code != "" {
			return &oe
		}
		return fmt.Errorf("auth: %s: http %d: %s", req.URL.Redacted(), resp.StatusCode, truncate(data, 256))
	}
	return json.Unmarshal(data, v)
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n]) + "..."
	}
	return string(b)
}
