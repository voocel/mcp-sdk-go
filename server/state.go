package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// SignState wraps an MRTR requestState payload with an HMAC-SHA256 tag. The
// spec requires integrity protection whenever inbound state influences
// authorization, resource access or business logic.
func (s *Server) SignState(payload []byte) (string, error) {
	if len(s.opts.StateKey) == 0 {
		return "", errors.New("server: SignState requires Options.StateKey")
	}
	mac := hmac.New(sha256.New, s.opts.StateKey)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VerifyState validates a state produced by SignState and returns the
// payload. Treat any error as an attacker-controlled state and reject the
// request.
func (s *Server) VerifyState(state string) ([]byte, error) {
	if len(s.opts.StateKey) == 0 {
		return nil, errors.New("server: VerifyState requires Options.StateKey")
	}
	payloadPart, tagPart, ok := strings.Cut(state, ".")
	if !ok {
		return nil, errors.New("server: malformed state")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return nil, errors.New("server: malformed state payload")
	}
	tag, err := base64.RawURLEncoding.DecodeString(tagPart)
	if err != nil {
		return nil, errors.New("server: malformed state tag")
	}
	mac := hmac.New(sha256.New, s.opts.StateKey)
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), tag) {
		return nil, errors.New("server: state integrity check failed")
	}
	return payload, nil
}
