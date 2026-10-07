package auth

import (
	"net/http"
	"regexp"
	"strings"
)

// Challenge is the Bearer challenge of a 401 or 403 response.
type Challenge struct {
	ResourceMetadata string
	Scope            string
	Error            string
}

var (
	bearerScheme = regexp.MustCompile(`(?i)(?:^|[\s,])Bearer(?:\s|$)`)
	// Matching every parameter, not just ours, consumes quoted values, so
	// an error_description saying scope=x is not read as a scope. An
	// unquoted value runs to the next comma or space: servers send
	// scope=files:write, though ":" is no token character.
	authParam  = regexp.MustCompile(`(?:^|[\s,])([\w.-]+)\s*=\s*(?:"((?:[^"\\]|\\.)*)"|([^\s,]+))`)
	quotedPair = regexp.MustCompile(`\\(.)`)
)

// ParseChallenge returns the Bearer challenge among h's WWW-Authenticate
// headers.
func ParseChallenge(h http.Header) (Challenge, bool) {
	for _, v := range h.Values("WWW-Authenticate") {
		loc := bearerScheme.FindStringIndex(v)
		if loc == nil {
			continue
		}
		var c Challenge
		for _, m := range authParam.FindAllStringSubmatch(v[loc[1]:], -1) {
			value := m[3]
			if value == "" {
				value = quotedPair.ReplaceAllString(m[2], "$1")
			}
			switch strings.ToLower(m[1]) {
			case "resource_metadata":
				c.ResourceMetadata = value
			case "scope":
				c.Scope = value
			case "error":
				c.Error = value
			}
		}
		return c, true
	}
	return Challenge{}, false
}
