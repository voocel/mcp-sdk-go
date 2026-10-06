package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// SignState wraps an MRTR requestState payload so it can round-trip through
// the client, which the spec treats as an attacker. The HMAC-SHA256 tag covers
// the payload, an expiry (Options.StateTTL) and the request the state was
// issued for — its method and target (tool or prompt name, resource URI) —
// plus any bind values, typically the authenticated principal. VerifyState
// rejects state replayed on another request, for another principal, or after
// it expires. Single use is not enforced: a state that may be redeemed once
// needs server-side bookkeeping.
func (s *Server) SignState(req *Request, payload []byte, bind ...string) (string, error) {
	if len(s.opts.StateKey) == 0 {
		return "", errors.New("server: SignState requires Options.StateKey")
	}
	body := binary.BigEndian.AppendUint64(nil, uint64(time.Now().Add(s.opts.StateTTL).UnixMilli()))
	body = append(body, payload...)
	return base64.RawURLEncoding.EncodeToString(body) + "." +
		base64.RawURLEncoding.EncodeToString(s.stateMAC(req, body, bind)), nil
}

// VerifyState validates a state produced by SignState for the same request and
// bind values, and returns the payload. Treat any error as attacker-controlled
// state and reject the request.
func (s *Server) VerifyState(req *Request, state string, bind ...string) ([]byte, error) {
	if len(s.opts.StateKey) == 0 {
		return nil, errors.New("server: VerifyState requires Options.StateKey")
	}
	bodyPart, tagPart, ok := strings.Cut(state, ".")
	if !ok {
		return nil, errors.New("server: malformed state")
	}
	body, err := base64.RawURLEncoding.DecodeString(bodyPart)
	if err != nil {
		return nil, errors.New("server: malformed state payload")
	}
	tag, err := base64.RawURLEncoding.DecodeString(tagPart)
	if err != nil {
		return nil, errors.New("server: malformed state tag")
	}
	if !hmac.Equal(s.stateMAC(req, body, bind), tag) {
		return nil, errors.New("server: state integrity check failed")
	}
	// Only this server can produce a valid tag, so body holds the expiry.
	if time.Now().UnixMilli() > int64(binary.BigEndian.Uint64(body)) {
		return nil, errors.New("server: state expired")
	}
	return body[8:], nil
}

// stateMAC tags body together with the scope it is valid in.
func (s *Server) stateMAC(req *Request, body []byte, bind []string) []byte {
	var target struct{ Name, URI string }
	_ = json.Unmarshal(req.rawParams, &target)
	scope, _ := json.Marshal(append([]string{req.method, target.Name, target.URI}, bind...))

	mac := hmac.New(sha256.New, s.opts.StateKey)
	mac.Write(binary.BigEndian.AppendUint32(nil, uint32(len(scope)))) // scope cannot bleed into body
	mac.Write(scope)
	mac.Write(body)
	return mac.Sum(nil)
}
