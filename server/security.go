package server

import (
	"strings"
	"sync"
	"time"
)

const (
	defaultSecurityTimeout    = 30 * time.Second
	defaultSecurityRateLimit  = 60
	defaultSecurityRateWindow = time.Minute
)

// SecurityDefaultsOptions provides one-click security defaults.
type SecurityDefaultsOptions struct {
	Timeout          time.Duration
	RateLimit        int
	RateWindow       time.Duration
	Tokens           []string
	DisableTimeout   bool
	DisableRateLimit bool
}

// ApplySecurityDefaults applies optional security defaults.
func ApplySecurityDefaults(s *Server, opts *SecurityDefaultsOptions) {
	if s == nil {
		return
	}

	cfg := SecurityDefaultsOptions{
		Timeout:    defaultSecurityTimeout,
		RateLimit:  defaultSecurityRateLimit,
		RateWindow: defaultSecurityRateWindow,
	}
	if opts != nil {
		if opts.DisableTimeout {
			cfg.Timeout = 0
		}
		if opts.DisableRateLimit {
			cfg.RateLimit = 0
		}
		if !opts.DisableTimeout && opts.Timeout != 0 {
			cfg.Timeout = opts.Timeout
		}
		if !opts.DisableRateLimit && opts.RateLimit != 0 {
			cfg.RateLimit = opts.RateLimit
		}
		if opts.RateWindow != 0 {
			cfg.RateWindow = opts.RateWindow
		}
		if len(opts.Tokens) > 0 {
			cfg.Tokens = opts.Tokens
		}
	}

	middlewares := make([]Middleware, 0, 4)
	middlewares = append(middlewares, RecoveryMiddleware())
	if cfg.Timeout > 0 {
		middlewares = append(middlewares, TimeoutMiddleware(cfg.Timeout))
	}
	if cfg.RateLimit > 0 && cfg.RateWindow > 0 {
		limiter := NewFixedWindowRateLimiter(cfg.RateLimit, cfg.RateWindow)
		middlewares = append(middlewares, RateLimitMiddleware(limiter))
	}
	if len(cfg.Tokens) > 0 {
		validator := NewTokenAuthValidator(cfg.Tokens)
		middlewares = append(middlewares, AuthMiddleware(validator))
	}

	if len(middlewares) > 0 {
		s.Use(middlewares...)
	}
}

// FixedWindowRateLimiter is a simple fixed-window rate limiter (per tool).
type FixedWindowRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	perTool map[string]*windowState
}

type windowState struct {
	count int
	reset time.Time
}

// NewFixedWindowRateLimiter creates a fixed-window rate limiter.
func NewFixedWindowRateLimiter(limit int, window time.Duration) *FixedWindowRateLimiter {
	return &FixedWindowRateLimiter{
		limit:   limit,
		window:  window,
		perTool: make(map[string]*windowState),
	}
}

// Allow implements RateLimiter
func (l *FixedWindowRateLimiter) Allow(tool string) bool {
	if l == nil || l.limit <= 0 || l.window <= 0 {
		return true
	}

	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	state := l.perTool[tool]
	if state == nil || now.After(state.reset) {
		state = &windowState{
			count: 0,
			reset: now.Add(l.window),
		}
		l.perTool[tool] = state
	}

	if state.count >= l.limit {
		return false
	}
	state.count++
	return true
}

// TokenAuthValidator is a simple token validator based on req.Params.Meta["auth"].
type TokenAuthValidator struct {
	tokens map[string]struct{}
}

// NewTokenAuthValidator creates a token validator.
func NewTokenAuthValidator(tokens []string) *TokenAuthValidator {
	set := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		if t == "" {
			continue
		}
		set[t] = struct{}{}
	}
	return &TokenAuthValidator{tokens: set}
}

// Validate implements AuthValidator
func (v *TokenAuthValidator) Validate(authInfo interface{}, tool string) bool {
	if v == nil || len(v.tokens) == 0 {
		return true
	}
	if authInfo == nil {
		return false
	}

	switch val := authInfo.(type) {
	case string:
		return v.hasToken(val)
	case map[string]any:
		if token, ok := val["token"].(string); ok {
			return v.hasToken(token)
		}
		if token, ok := val["bearer"].(string); ok {
			return v.hasToken(token)
		}
	}

	return false
}

func (v *TokenAuthValidator) hasToken(token string) bool {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	}
	_, ok := v.tokens[token]
	return ok
}
