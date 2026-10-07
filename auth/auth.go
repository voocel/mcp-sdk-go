// Package auth is the client side of MCP authorization over HTTP: it finds
// the server's authorization server, runs the authorization code flow with
// PKCE on a loopback redirect, and sends and refreshes the token on every
// request. The client is either registered with the authorization server
// beforehand or identified by its Client ID Metadata Document.
package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// Store persists tokens by MCP server URL. They are secrets.
type Store interface {
	Token(server string) (*Token, error)     // nil when absent
	SaveToken(server string, t *Token) error // nil deletes
}

// Token is an access token for one MCP server.
type Token struct {
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"client_id"` // the client it was issued to
	Resource     string    `json:"resource"`  // the RFC 8707 resource it was issued for
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
	Scope        string    `json:"scope,omitempty"`
}

// expiryMargin refreshes a token this long before it expires, so a request
// does not leave with a token that expires on the way.
const expiryMargin = 30 * time.Second

func (t *Token) fresh(now time.Time) bool {
	return t.Expiry.IsZero() || now.Add(expiryMargin).Before(t.Expiry)
}

type Config struct {
	Server string // the MCP endpoint URL
	Store  Store
	// ClientID, with ClientSecret if it has one, is a client registered
	// beforehand with the server's authorization server, its redirect URI
	// http://127.0.0.1/callback on any port (RFC 8252 Section 7.3).
	ClientID     string
	ClientSecret string
	// ClientMetadataURL is the https URL of the client's Client ID Metadata
	// Document, its client_id with authorization servers that accept one.
	// The document lists the loopback redirect URIs to listen on.
	ClientMetadataURL string
	// HTTPClient makes the metadata and token requests; nil means a client
	// with a 30-second timeout.
	HTTPClient *http.Client
}

// Authorizer holds the authorization of one MCP server. It is safe for
// concurrent use.
type Authorizer struct {
	cfg  Config
	http *http.Client

	mu        sync.Mutex
	token     *Token // cached from the store
	challenge Challenge
}

func New(cfg Config) *Authorizer {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Authorizer{cfg: cfg, http: hc}
}

// Transport returns a RoundTripper that sends the stored token, refreshing
// it when it expires or the server rejects it. When no token works, the
// server's 401 or 403 is returned as is; Login then asks the user.
func (a *Authorizer) Transport(base http.RoundTripper) http.RoundTripper {
	return &roundTripper{a: a, base: base}
}

// Logout forgets the token.
func (a *Authorizer) Logout() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.token = nil
	return a.cfg.Store.SaveToken(a.cfg.Server, nil)
}

type roundTripper struct {
	a    *Authorizer
	base http.RoundTripper
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := rt.a.current(req.Context())
	if err != nil {
		return nil, err
	}
	resp, err := rt.send(req, tok)
	if err != nil || (resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden) {
		return resp, err
	}
	rt.a.observe(resp.Header)
	if resp.StatusCode != http.StatusUnauthorized || tok == nil || (req.Body != nil && req.GetBody == nil) {
		return resp, nil
	}
	retry, err := rt.a.replace(req.Context(), tok)
	if retry == nil {
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
	req = req.Clone(req.Context())
	if req.GetBody != nil {
		if req.Body, err = req.GetBody(); err != nil {
			return nil, err
		}
	}
	return rt.send(req, retry)
}

func (rt *roundTripper) send(req *http.Request, tok *Token) (*http.Response, error) {
	if tok != nil {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	}
	return rt.base.RoundTrip(req)
}

// current returns the token to send, refreshed if it has expired, or nil.
func (a *Authorizer) current(ctx context.Context) (*Token, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if a.token != nil && a.token.fresh(now) {
		return a.token, nil
	}
	t, err := a.cfg.Store.Token(a.cfg.Server)
	if err != nil || t == nil {
		return nil, err
	}
	if !t.fresh(now) && t.RefreshToken != "" {
		// A refused refresh sends the expired token, whose 401 asks for a
		// login.
		switch r, err := a.refresh(ctx, t); {
		case err == nil:
			t = r
		case !refused(err):
			return nil, err
		}
	}
	a.token = t
	return t, nil
}

// replace returns a token to retry with after the server rejected one: a
// newer one another process stored, or a refreshed one. It returns nil and
// no error when only a login helps.
func (a *Authorizer) replace(ctx context.Context, rejected *Token) (*Token, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.token = nil
	t, err := a.cfg.Store.Token(a.cfg.Server)
	if err != nil || t == nil {
		return nil, err
	}
	if t.AccessToken != rejected.AccessToken {
		a.token = t
		return t, nil
	}
	if t.RefreshToken == "" {
		return nil, nil
	}
	r, err := a.refresh(ctx, t)
	if refused(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.token = r
	return r, nil
}

// refused reports whether err is the authorization server refusing a
// refresh token.
func refused(err error) bool {
	var oe *oauthError
	return errors.As(err, &oe)
}

func (a *Authorizer) observe(h http.Header) {
	c, ok := ParseChallenge(h)
	if !ok {
		return
	}
	a.mu.Lock()
	a.challenge = c
	a.mu.Unlock()
}
