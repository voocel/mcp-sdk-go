package server

import (
	"context"
	"maps"

	"github.com/voocel/mcp-sdk-go/protocol"
)

func (s *Server) handleDiscover(ctx context.Context, req *Request) (protocol.Result, error) {
	res := &protocol.DiscoverResult{
		SupportedVersions: []string{protocol.Version},
		Capabilities:      s.capabilities(),
		Instructions:      s.opts.Instructions,
	}
	if s.opts.OnDiscover != nil {
		if err := s.opts.OnDiscover(ctx, req, res); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// capabilities derives the advertisement from what is actually registered, so
// it can never lie.
func (s *Server) capabilities() protocol.ServerCapabilities {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var caps protocol.ServerCapabilities
	if s.toolsDeclared {
		caps.Tools = &protocol.ToolsCapability{ListChanged: true}
	}
	if s.promptsDeclared {
		caps.Prompts = &protocol.PromptsCapability{ListChanged: true}
	}
	if s.resourcesDeclared {
		caps.Resources = &protocol.ResourcesCapability{Subscribe: true, ListChanged: true}
	}
	if s.opts.CompletionHandler != nil {
		caps.Completions = &struct{}{}
	}
	if len(s.extSettings) > 0 {
		caps.Extensions = maps.Clone(s.extSettings)
	}
	return caps
}
