package auth_test

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/voocel/mcp-sdk-go/auth"
	"github.com/voocel/mcp-sdk-go/client"
	"github.com/voocel/mcp-sdk-go/protocol"
	"github.com/voocel/mcp-sdk-go/server"
	"github.com/voocel/mcp-sdk-go/transport/streamhttp"
)

type memStore struct {
	mu     sync.Mutex
	tokens map[string]*auth.Token
	err    error // what reading a token fails with
}

func newMemStore() *memStore {
	return &memStore{tokens: map[string]*auth.Token{}}
}

func (s *memStore) Token(server string) (*auth.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[server], s.err
}

func (s *memStore) SaveToken(server string, t *auth.Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t == nil {
		delete(s.tokens, server)
	} else {
		s.tokens[server] = t
	}
	return nil
}

// The client registered with fakeAS beforehand.
const (
	registeredID     = "registered-1"
	registeredSecret = "s3cr:t"
)

// fakeAS is an authorization server that checks what the MCP specification
// requires of a client.
type fakeAS struct {
	t   *testing.T
	srv *httptest.Server

	mu          sync.Mutex
	metadata    map[string]any // overrides
	expiresIn   int
	errorStatus int                        // of error responses
	iss         func(issuer string) string // the iss to send back; nil sends the issuer
	codes       map[string]grant
	access      map[string]bool // valid access tokens
	refresh     map[string]grant
	refreshes   int
	requests    []grant  // of each authorization request
	methods     []string // client authentication of each token request
	n           int
}

type grant struct {
	client, challenge, redirect, resource, scope string
}

func newFakeAS(t *testing.T) *fakeAS {
	as := &fakeAS{t: t, expiresIn: 3600, errorStatus: http.StatusBadRequest, metadata: map[string]any{}, codes: map[string]grant{}, access: map[string]bool{}, refresh: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		as.mu.Lock()
		defer as.mu.Unlock()
		m := map[string]any{
			"issuer":                           as.srv.URL,
			"authorization_endpoint":           as.srv.URL + "/authorize",
			"token_endpoint":                   as.srv.URL + "/token",
			"code_challenge_methods_supported": []string{"S256"},
			"authorization_response_iss_parameter_supported": true,
			"client_id_metadata_document_supported":          true,
			"scopes_supported":                               []string{"read", "write", "offline_access"},
		}
		for k, v := range as.metadata {
			m[k] = v
		}
		writeJSON(w, m)
	})
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("state") == "" || q.Get("resource") == "" {
			t.Errorf("authorization request %v", q)
		}
		if !accepts(q.Get("client_id"), q.Get("redirect_uri")) {
			t.Errorf("authorization request of %q to %q", q.Get("client_id"), q.Get("redirect_uri"))
			http.Error(w, "unknown client", http.StatusBadRequest)
			return
		}
		as.mu.Lock()
		as.n++
		code := fmt.Sprintf("code-%d", as.n)
		as.codes[code] = grant{q.Get("client_id"), q.Get("code_challenge"), q.Get("redirect_uri"), q.Get("resource"), q.Get("scope")}
		as.requests = append(as.requests, as.codes[code])
		iss := as.srv.URL
		if as.iss != nil {
			iss = as.iss(iss)
		}
		as.mu.Unlock()
		back := url.Values{"code": {code}, "state": {q.Get("state")}}
		if iss != "" {
			back.Set("iss", iss)
		}
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+back.Encode(), http.StatusFound)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		as.mu.Lock()
		defer as.mu.Unlock()
		fail := func(code string) {
			w.WriteHeader(as.errorStatus)
			writeJSON(w, map[string]any{"error": code})
		}
		id, secret, method := r.Form.Get("client_id"), r.Form.Get("client_secret"), "none"
		if u, p, ok := r.BasicAuth(); ok {
			id, _ = url.QueryUnescape(u)
			secret, _ = url.QueryUnescape(p)
			method = "client_secret_basic"
		} else if secret != "" {
			method = "client_secret_post"
		}
		as.methods = append(as.methods, method)
		want := ""
		if id == registeredID {
			want = registeredSecret
		}
		if secret != want {
			fail("invalid_client")
			return
		}
		var g grant
		switch r.Form.Get("grant_type") {
		case "authorization_code":
			var ok bool
			g, ok = as.codes[r.Form.Get("code")]
			delete(as.codes, r.Form.Get("code"))
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if !ok || g.challenge != base64.RawURLEncoding.EncodeToString(sum[:]) || g.redirect != r.Form.Get("redirect_uri") || g.resource != r.Form.Get("resource") {
				fail("invalid_grant")
				return
			}
		case "refresh_token":
			var ok bool
			g, ok = as.refresh[r.Form.Get("refresh_token")]
			delete(as.refresh, r.Form.Get("refresh_token")) // rotation
			if !ok || g.resource != r.Form.Get("resource") {
				fail("invalid_grant")
				return
			}
			as.refreshes++
		}
		if g.client != id {
			t.Errorf("token request of %q for a grant to %q", id, g.client)
		}
		as.n++
		at, rt := fmt.Sprintf("access-%d", as.n), fmt.Sprintf("refresh-%d", as.n)
		as.access[at] = true
		as.refresh[rt] = g
		writeJSON(w, map[string]any{"access_token": at, "token_type": "bearer", "expires_in": as.expiresIn, "refresh_token": rt, "scope": g.scope})
	})
	as.srv = httptest.NewServer(mux)
	t.Cleanup(as.srv.Close)
	return as
}

// accepts reports whether redirect is a redirect URI of the client: of the
// client registered beforehand, http://127.0.0.1/callback on any port; of a
// client with a metadata document, one listed there.
func accepts(clientID, redirect string) bool {
	if clientID == registeredID {
		u, err := url.Parse(redirect)
		return err == nil && u.Scheme == "http" && u.Hostname() == "127.0.0.1" && u.Path == "/callback"
	}
	resp, err := http.Get(clientID)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var doc struct {
		ClientID     string   `json:"client_id"`
		RedirectURIs []string `json:"redirect_uris"`
	}
	return json.NewDecoder(resp.Body).Decode(&doc) == nil && doc.ClientID == clientID && slices.Contains(doc.RedirectURIs, redirect)
}

// clientDocument serves a Client ID Metadata Document listing loopback
// redirect URIs on ports that were free.
type clientDocument struct {
	url       string
	id        string // the client_id it claims; "" for its URL
	redirects []string
}

func newClientDocument(t *testing.T) *clientDocument {
	d := &clientDocument{}
	for range 2 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		d.redirects = append(d.redirects, "http://"+ln.Addr().String()+"/callback")
		ln.Close()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"client_id":                  cmp.Or(d.id, d.url),
			"client_name":                "test",
			"redirect_uris":              d.redirects,
			"token_endpoint_auth_method": "none",
		})
	}))
	t.Cleanup(srv.Close)
	d.url = srv.URL + "/client.json"
	return d
}

func (as *fakeAS) valid(token string) bool {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.access[token]
}

func (as *fakeAS) revoke(token string) {
	as.mu.Lock()
	defer as.mu.Unlock()
	delete(as.access, token)
}

// fakeRS is an MCP server that wants a token from as.
type fakeRS struct {
	srv      *httptest.Server
	resource string // what its metadata claims; "" for its own endpoint
	header   bool   // whether its challenge points to the metadata
	need     string // a scope every token must have been granted
	as       *fakeAS
}

func newFakeRS(t *testing.T, as *fakeAS, tune func(*fakeRS)) *fakeRS {
	rs := &fakeRS{header: true, as: as}
	if tune != nil {
		tune(rs)
	}
	srv := server.New(&server.Options{Impl: protocol.Implementation{Name: "protected", Version: "1"}})
	mcp := streamhttp.NewHandler(srv, nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"resource":              cmp.Or(rs.resource, rs.srv.URL+"/mcp"),
			"authorization_servers": []string{as.srv.URL},
			"scopes_supported":      []string{"read"},
		})
	})
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		challenge := `Bearer realm="test"`
		if rs.header {
			challenge += `, resource_metadata="` + rs.srv.URL + `/.well-known/oauth-protected-resource/mcp", scope="read"`
		}
		if !as.valid(token) {
			w.Header().Set("WWW-Authenticate", challenge)
			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
			return
		}
		if rs.need != "" && !as.granted(token, rs.need) {
			w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+rs.need+`", resource_metadata="`+rs.srv.URL+`/.well-known/oauth-protected-resource/mcp"`)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mcp.ServeHTTP(w, r)
	})
	rs.srv = httptest.NewServer(mux)
	t.Cleanup(rs.srv.Close)
	return rs
}

func (as *fakeAS) granted(token, scope string) bool {
	as.mu.Lock()
	defer as.mu.Unlock()
	// The access and refresh tokens of one grant share their number.
	g, ok := as.refresh["refresh-"+strings.TrimPrefix(token, "access-")]
	return ok && strings.Contains(" "+g.scope+" ", " "+scope+" ")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

type harness struct {
	t     *testing.T
	as    *fakeAS
	rs    *fakeRS
	doc   *clientDocument
	store *memStore
	authz *auth.Authorizer
	mcp   *client.Client
}

// newHarness has a client with a metadata document talk to an MCP server.
func newHarness(t *testing.T, tune func(*fakeRS)) *harness {
	h := &harness{t: t, as: newFakeAS(t), doc: newClientDocument(t), store: newMemStore()}
	h.rs = newFakeRS(t, h.as, tune)
	h.connect(auth.Config{ClientMetadataURL: h.doc.url})
	return h
}

// connect has the MCP client authorize as cfg's client.
func (h *harness) connect(cfg auth.Config) {
	cfg.Server = h.rs.srv.URL + "/mcp"
	cfg.Store = h.store
	h.authz = auth.New(cfg)
	h.mcp = client.New(streamhttp.New(cfg.Server, &streamhttp.TransportOptions{
		HTTPClient: &http.Client{Transport: h.authz.Transport(http.DefaultTransport)},
		MaxRetries: -1,
	}), nil)
}

func (h *harness) discover() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := h.mcp.Discover(ctx)
	return err
}

// login authorizes as a user who agrees at once.
func (h *harness) login() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	l, err := h.authz.Login(ctx)
	if err != nil {
		return err
	}
	go func() {
		resp, err := http.Get(l.URL) // the browser follows the redirect home
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	return l.Wait(ctx)
}

func statusOf(err error) int {
	var se *streamhttp.StatusError
	if errors.As(err, &se) {
		return se.StatusCode
	}
	return 0
}

func TestLoginThenRequestsCarryTheToken(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.discover(); statusOf(err) != http.StatusUnauthorized {
		t.Fatalf("before login: %v", err)
	}
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	if err := h.discover(); err != nil {
		t.Fatalf("after login: %v", err)
	}
	tok, _ := h.store.Token(h.rs.srv.URL + "/mcp")
	if tok == nil || tok.Issuer != h.as.srv.URL || tok.ClientID != h.doc.url || tok.Resource != h.rs.srv.URL+"/mcp" {
		t.Fatalf("stored token %+v", tok)
	}
	if got := h.as.requests[0].scope; got != "read offline_access" {
		t.Errorf("requested scope %q", got)
	}
	if !slices.Equal(h.as.methods, []string{"none"}) {
		t.Errorf("client authentication %q", h.as.methods)
	}
}

func TestAClientRegisteredBeforehandComesFirst(t *testing.T) {
	for _, tc := range []struct {
		methods []string // the authorization server's
		want    string
	}{
		{nil, "client_secret_basic"},
		{[]string{"client_secret_basic", "client_secret_post"}, "client_secret_basic"},
		{[]string{"none", "client_secret_post"}, "client_secret_post"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			h := newHarness(t, nil)
			if tc.methods != nil {
				h.as.metadata["token_endpoint_auth_methods_supported"] = tc.methods
			}
			h.as.expiresIn = 10 // every request refreshes
			h.connect(auth.Config{ClientID: registeredID, ClientSecret: registeredSecret, ClientMetadataURL: h.doc.url})
			h.discover()
			if err := h.login(); err != nil {
				t.Fatal(err)
			}
			if err := h.discover(); err != nil {
				t.Fatal(err)
			}
			if tok, _ := h.store.Token(h.rs.srv.URL + "/mcp"); tok.ClientID != registeredID {
				t.Errorf("token of %q", tok.ClientID)
			}
			if !slices.Equal(h.as.methods, []string{tc.want, tc.want}) {
				t.Errorf("client authentication %q", h.as.methods)
			}
		})
	}
}

func TestATakenRedirectPortGivesWayToTheNext(t *testing.T) {
	h := newHarness(t, nil)
	u, _ := url.Parse(h.doc.redirects[0])
	ln, err := net.Listen("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	h.discover()
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	if got := h.as.requests[0].redirect; got != h.doc.redirects[1] {
		t.Errorf("redirected to %q", got)
	}
}

func TestNoClientForAnAuthorizationServerWithoutDocuments(t *testing.T) {
	h := newHarness(t, nil)
	h.as.metadata["client_id_metadata_document_supported"] = false
	h.discover()
	if _, err := h.authz.Login(context.Background()); !errors.Is(err, auth.ErrClientRequired) {
		t.Fatalf("got %v", err)
	}
}

func TestExpiringTokensAreRefreshed(t *testing.T) {
	h := newHarness(t, nil)
	h.as.expiresIn = 10 // inside the refresh margin: every request refreshes
	h.discover()
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := h.discover(); err != nil {
			t.Fatal(err)
		}
	}
	if h.as.refreshes != 3 {
		t.Errorf("%d refreshes", h.as.refreshes)
	}
}

func TestARejectedTokenIsRefreshedAndRetried(t *testing.T) {
	h := newHarness(t, nil)
	h.discover()
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	tok, _ := h.store.Token(h.rs.srv.URL + "/mcp")
	h.as.revoke(tok.AccessToken)
	if err := h.discover(); err != nil {
		t.Fatal(err)
	}
	if h.as.refreshes != 1 {
		t.Errorf("%d refreshes", h.as.refreshes)
	}
}

func TestARefusedRefreshAsksForALogin(t *testing.T) {
	// GitHub refuses with 200.
	for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			h := newHarness(t, nil)
			h.as.errorStatus = status
			h.discover()
			if err := h.login(); err != nil {
				t.Fatal(err)
			}
			tok, _ := h.store.Token(h.rs.srv.URL + "/mcp")
			h.as.revoke(tok.AccessToken)
			h.as.mu.Lock()
			clear(h.as.refresh)
			h.as.mu.Unlock()
			if err := h.discover(); statusOf(err) != http.StatusUnauthorized {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestAStoreThatFailsIsNoLoginToAskFor(t *testing.T) {
	h := newHarness(t, nil)
	h.discover()
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	tok, _ := h.store.Token(h.rs.srv.URL + "/mcp")
	h.as.revoke(tok.AccessToken)
	h.store.mu.Lock()
	h.store.err = errors.New("disk on fire")
	h.store.mu.Unlock()
	if err := h.discover(); err == nil || statusOf(err) != 0 || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("got %v", err)
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t, nil)
	h.discover()
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	if err := h.authz.Logout(); err != nil {
		t.Fatal(err)
	}
	if err := h.discover(); statusOf(err) != http.StatusUnauthorized {
		t.Fatalf("got %v", err)
	}
}

func TestMetadataIsFoundWithoutTheChallenge(t *testing.T) {
	h := newHarness(t, func(rs *fakeRS) { rs.header = false })
	h.discover()
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	if err := h.discover(); err != nil {
		t.Fatal(err)
	}
}

func TestStepUpKeepsTheGrantedScopes(t *testing.T) {
	h := newHarness(t, nil)
	h.discover()
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	h.rs.need = "write"
	if err := h.discover(); statusOf(err) != http.StatusForbidden {
		t.Fatalf("got %v", err)
	}
	if err := h.login(); err != nil {
		t.Fatal(err)
	}
	if got := h.as.requests[1].scope; got != "write read offline_access" {
		t.Errorf("step-up scope %q", got)
	}
	if err := h.discover(); err != nil {
		t.Fatal(err)
	}
}

func TestLoginRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		tune func(*harness)
		want string
	}{
		{"no PKCE", func(h *harness) { h.as.metadata["code_challenge_methods_supported"] = []string{"plain"} }, "PKCE"},
		{"another issuer", func(h *harness) { h.as.metadata["issuer"] = "https://honest.example" }, "claims to be"},
		{"another resource", func(h *harness) { h.rs.resource = "https://elsewhere.example/mcp" }, "describes"},
		{"plain http endpoint", func(h *harness) { h.as.metadata["token_endpoint"] = "http://auth.example/token" }, "not an https URL"},
		{"no document", func(h *harness) { h.connect(auth.Config{ClientMetadataURL: h.as.srv.URL + "/client.json"}) }, "serves no client metadata document"},
		{"a document of another client", func(h *harness) { h.doc.id = "https://other.example/client.json" }, "is of \"https://other.example/client.json\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			tc.tune(h)
			h.discover()
			_, err := h.authz.Login(context.Background())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTheAuthorizationResponseMustComeFromTheIssuer(t *testing.T) {
	for _, tc := range []struct {
		name string
		iss  func(string) string
		ok   bool
	}{
		{"another issuer", func(string) string { return "https://attacker.example" }, false},
		{"missing though advertised", func(string) string { return "" }, false},
		{"the issuer", func(s string) string { return s }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, nil)
			h.as.iss = tc.iss
			h.discover()
			err := h.login()
			if (err == nil) != tc.ok {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestACallbackWithAnotherStateIsIgnored(t *testing.T) {
	h := newHarness(t, nil)
	h.discover()
	l, err := h.authz.Login(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	q, _ := url.ParseQuery(l.URL[strings.Index(l.URL, "?")+1:])
	resp, err := http.Get(q.Get("redirect_uri") + "?code=forged&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("forged callback: %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait after a forged callback: %v", err)
	}
}

func TestParseChallenge(t *testing.T) {
	for _, tc := range []struct {
		header []string
		want   auth.Challenge
		ok     bool
	}{
		{[]string{`Bearer resource_metadata="https://a.example/m", scope="read write"`}, auth.Challenge{ResourceMetadata: "https://a.example/m", Scope: "read write"}, true},
		{[]string{`Basic realm="x", Bearer error="insufficient_scope", scope=files:write`}, auth.Challenge{Error: "insufficient_scope", Scope: "files:write"}, true},
		{[]string{`Negotiate abc==, Bearer scope="a\"b"`}, auth.Challenge{Scope: `a"b`}, true},
		{[]string{`Bearer error="insufficient_scope", error_description="needs scope=admin", scope="write"`}, auth.Challenge{Error: "insufficient_scope", Scope: "write"}, true},
		{[]string{`Bearer scope="write", error_description="needs scope=admin"`}, auth.Challenge{Scope: "write"}, true},
		{[]string{`Bearer`}, auth.Challenge{}, true},
		{[]string{`Basic realm="x"`, `bearer scope="s"`}, auth.Challenge{Scope: "s"}, true},
		{[]string{`Basic realm="x"`}, auth.Challenge{}, false},
	} {
		h := http.Header{"Www-Authenticate": tc.header}
		got, ok := auth.ParseChallenge(h)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: got %+v %v, want %+v %v", tc.header, got, ok, tc.want, tc.ok)
		}
	}
}
