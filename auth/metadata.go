package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// resourceMetadata is OAuth 2.0 Protected Resource Metadata (RFC 9728).
type resourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

// serverMetadata is OAuth 2.0 Authorization Server Metadata (RFC 8414) or
// OpenID Connect Discovery.
type serverMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	TokenEndpointAuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported               []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	IssParameterSupported         bool     `json:"authorization_response_iss_parameter_supported"`
	ClientMetadataDocuments       bool     `json:"client_id_metadata_document_supported"`
}

// resourceMetadata fetches the server's metadata from where its challenge
// says, or else from the well-known URIs, path-specific first.
func (a *Authorizer) resourceMetadata(ctx context.Context, ch Challenge) (*resourceMetadata, error) {
	urls := []string{ch.ResourceMetadata}
	if ch.ResourceMetadata == "" {
		var err error
		if urls, err = wellKnown(a.cfg.Server, "oauth-protected-resource"); err != nil {
			return nil, err
		}
	}
	for _, u := range urls {
		var m resourceMetadata
		found, err := a.getJSON(ctx, u, &m)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if len(m.AuthorizationServers) == 0 {
			return nil, fmt.Errorf("auth: %s names no authorization server", u)
		}
		// Otherwise a server could have the user authorize, and hand it
		// tokens for, another resource.
		if !covers(m.Resource, a.cfg.Server) {
			return nil, fmt.Errorf("auth: %s describes %q, not %s", u, m.Resource, a.cfg.Server)
		}
		return &m, nil
	}
	return nil, fmt.Errorf("auth: %s publishes no protected resource metadata", a.cfg.Server)
}

// serverMetadata fetches the metadata of the authorization server issuer,
// trying the URLs the MCP specification lists in its order.
func (a *Authorizer) serverMetadata(ctx context.Context, issuer string) (*serverMetadata, error) {
	u, err := secureURL(issuer)
	if err != nil {
		return nil, err
	}
	origin := u.Scheme + "://" + u.Host
	path := strings.TrimSuffix(u.Path, "/")
	urls := []string{
		origin + "/.well-known/oauth-authorization-server" + path,
		origin + "/.well-known/openid-configuration" + path,
	}
	if path != "" {
		urls = append(urls, origin+path+"/.well-known/openid-configuration")
	}
	for _, mu := range urls {
		var m serverMetadata
		found, err := a.getJSON(ctx, mu, &m)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if m.Issuer != issuer {
			return nil, fmt.Errorf("auth: %s claims to be %q, not %q", mu, m.Issuer, issuer)
		}
		if !slices.Contains(m.CodeChallengeMethodsSupported, "S256") {
			return nil, fmt.Errorf("auth: %s does not support PKCE with S256", issuer)
		}
		for _, e := range []string{m.AuthorizationEndpoint, m.TokenEndpoint} {
			if _, err := secureURL(e); err != nil {
				return nil, err
			}
		}
		return &m, nil
	}
	return nil, fmt.Errorf("auth: %s publishes no authorization server metadata", issuer)
}

// wellKnown returns the well-known URIs of server for suffix: with the
// server's path appended (RFC 9728 Section 3.1), then at the root.
func wellKnown(server, suffix string) ([]string, error) {
	u, err := secureURL(server)
	if err != nil {
		return nil, err
	}
	root := u.Scheme + "://" + u.Host + "/.well-known/" + suffix
	if u.Path == "" || u.Path == "/" {
		return []string{root}, nil
	}
	withPath := root + u.Path
	if u.RawQuery != "" {
		withPath += "?" + u.RawQuery
	}
	return []string{withPath, root}, nil
}

// covers reports whether resource identifies server: same origin, and a
// path that is server's or a parent of it.
func covers(resource, server string) bool {
	r, err := url.Parse(resource)
	if err != nil || r.Fragment != "" || r.Host == "" {
		return false
	}
	s, err := url.Parse(server)
	if err != nil {
		return false
	}
	if !strings.EqualFold(r.Scheme, s.Scheme) || !strings.EqualFold(r.Host, s.Host) {
		return false
	}
	rp, sp := strings.TrimSuffix(r.Path, "/"), strings.TrimSuffix(s.Path, "/")
	return rp == sp || strings.HasPrefix(sp, rp+"/")
}

// secureURL requires https, except on loopback hosts.
func secureURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	if u.Host != "" && (u.Scheme == "https" || u.Scheme == "http" && isLoopback(u.Hostname())) {
		return u, nil
	}
	return nil, fmt.Errorf("auth: %q is not an https URL", raw)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

const maxBody = 1 << 20

// getJSON decodes the JSON document at u into v; found is false when the
// server has none there.
func (a *Authorizer) getJSON(ctx context.Context, u string, v any) (found bool, err error) {
	if _, err := secureURL(u); err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return false, fmt.Errorf("auth: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return false, fmt.Errorf("auth: %s: %w", u, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, fmt.Errorf("auth: %s: %w", u, err)
	}
	return true, nil
}
