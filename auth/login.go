package auth

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Login is an authorization waiting for the user.
type Login struct {
	// URL is the authorization page the user must open in a browser.
	URL string

	a        *Authorizer
	as       *serverMetadata
	client   client
	redirect string
	resource string
	scope    string
	verifier string
	state    string
	srv      *http.Server
	once     sync.Once
	done     chan callback
}

type callback struct {
	code string
	err  error
}

// Login starts an authorization: it finds the authorization server and
// listens for the redirect on a loopback port. Wait completes it.
func (a *Authorizer) Login(ctx context.Context) (*Login, error) {
	a.mu.Lock()
	ch := a.challenge
	a.mu.Unlock()

	rm, err := a.resourceMetadata(ctx, ch)
	if err != nil {
		return nil, err
	}
	as, err := a.serverMetadata(ctx, rm.AuthorizationServers[0])
	if err != nil {
		return nil, err
	}
	scope, err := a.scope(ch, rm, as)
	if err != nil {
		return nil, err
	}
	l := &Login{
		a:        a,
		as:       as,
		resource: rm.Resource,
		scope:    scope,
		verifier: random(32),
		state:    random(16),
		done:     make(chan callback, 1),
	}
	ln, err := l.listen(ctx)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(l.verifier))
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {l.client.id},
		"redirect_uri":          {l.redirect},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {l.state},
		"resource":              {l.resource},
	}
	if scope != "" {
		q.Set("scope", scope)
	}
	sep := "?"
	if strings.Contains(as.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	l.URL = as.AuthorizationEndpoint + sep + q.Encode()
	l.srv = &http.Server{Handler: http.HandlerFunc(l.callback), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = l.srv.Serve(ln) }()
	return l, nil
}

// Wait waits for the browser to come back, redeems the code and stores the
// token.
func (l *Login) Wait(ctx context.Context) error {
	defer l.srv.Close()
	var cb callback
	select {
	case cb = <-l.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if cb.err != nil {
		return cb.err
	}
	t, err := l.a.requestToken(ctx, l.as, l.client, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {cb.code},
		"redirect_uri":  {l.redirect},
		"code_verifier": {l.verifier},
		"resource":      {l.resource},
	})
	if err != nil {
		return err
	}
	t.Scope = cmp.Or(t.Scope, l.scope)
	l.a.mu.Lock()
	defer l.a.mu.Unlock()
	if err := l.a.cfg.Store.SaveToken(l.a.cfg.Server, t); err != nil {
		return err
	}
	l.a.token = t
	l.a.challenge = Challenge{}
	return nil
}

func (l *Login) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// Anything else that reaches the port, or a response to another
	// request, is not ours to act on.
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(l.state)) != 1 {
		http.NotFound(w, r)
		return
	}
	cb := l.check(q)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if cb.err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Authorization failed: %v\n", cb.err)
	} else {
		fmt.Fprintln(w, "Authorized. You can close this window.")
	}
	l.once.Do(func() { l.done <- cb })
}

// check validates the authorization response, its issuer first (RFC 9207):
// a response from another authorization server is not acted on, not even
// to show its error.
func (l *Login) check(q url.Values) callback {
	iss, present := q["iss"]
	switch {
	case present && iss[0] != l.as.Issuer:
		return callback{err: fmt.Errorf("auth: the authorization response comes from %q, not %q", iss[0], l.as.Issuer)}
	case !present && l.as.IssParameterSupported:
		return callback{err: fmt.Errorf("auth: the authorization response from %s does not say who sent it", l.as.Issuer)}
	}
	if e := q.Get("error"); e != "" {
		return callback{err: &oauthError{Code: e, Description: q.Get("error_description")}}
	}
	code := q.Get("code")
	if code == "" {
		return callback{err: errors.New("auth: the authorization response has no code")}
	}
	return callback{code: code}
}

// ErrClientRequired is the authorization server taking only clients
// registered with it beforehand, and Config naming none.
var ErrClientRequired = errors.New("auth: the authorization server needs a client registered with it")

// listen picks the client to log in as, one registered beforehand first,
// then the client metadata document, and listens on its redirect URI.
func (l *Login) listen(ctx context.Context) (net.Listener, error) {
	cfg := l.a.cfg
	switch {
	case cfg.ClientID != "":
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		l.client = l.a.clientFor(cfg.ClientID)
		l.redirect = "http://" + ln.Addr().String() + "/callback"
		return ln, nil
	case cfg.ClientMetadataURL == "" || !l.as.ClientMetadataDocuments:
		return nil, fmt.Errorf("%w: %s", ErrClientRequired, l.as.Issuer)
	}
	var doc struct {
		ClientID     string   `json:"client_id"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	found, err := l.a.getJSON(ctx, cfg.ClientMetadataURL, &doc)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("auth: %s serves no client metadata document", cfg.ClientMetadataURL)
	}
	if doc.ClientID != cfg.ClientMetadataURL {
		return nil, fmt.Errorf("auth: the client metadata document at %s is of %q", cfg.ClientMetadataURL, doc.ClientID)
	}
	// The authorization server matches the redirect URI exactly, port and
	// all, so it is one of those listed that is free.
	for _, r := range doc.RedirectURIs {
		u, err := url.Parse(r)
		if err != nil || u.Scheme != "http" || !isLoopback(u.Hostname()) {
			continue
		}
		if ln, err := net.Listen("tcp", u.Host); err == nil {
			l.client = client{id: cfg.ClientMetadataURL}
			l.redirect = r
			return ln, nil
		}
	}
	return nil, fmt.Errorf("auth: no loopback redirect URI of %s is free", cfg.ClientMetadataURL)
}

// scope picks the scopes to request: those the server's challenge asks for,
// keeping those already granted when it asks for more, or else those its
// metadata lists. offline_access, where the authorization server knows it,
// asks for a refresh token.
func (a *Authorizer) scope(ch Challenge, rm *resourceMetadata, as *serverMetadata) (string, error) {
	scopes := rm.ScopesSupported
	if ch.Scope != "" {
		scopes = strings.Fields(ch.Scope)
		if ch.Error == "insufficient_scope" {
			prev, err := a.cfg.Store.Token(a.cfg.Server)
			if err != nil {
				return "", err
			}
			if prev != nil {
				for _, s := range strings.Fields(prev.Scope) {
					if !slices.Contains(scopes, s) {
						scopes = append(scopes, s)
					}
				}
			}
		}
	}
	if len(scopes) > 0 && slices.Contains(as.ScopesSupported, "offline_access") && !slices.Contains(scopes, "offline_access") {
		scopes = append(slices.Clip(scopes), "offline_access")
	}
	return strings.Join(scopes, " "), nil
}

func random(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
