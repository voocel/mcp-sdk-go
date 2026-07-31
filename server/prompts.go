package server

import (
	"context"
	"fmt"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// PromptHandler resolves prompts/get. Return *protocol.GetPromptResult or an
// MRTR interim result via protocol.RequireInput.
type PromptHandler func(ctx context.Context, req *PromptRequest) (protocol.PromptResponse, error)

type serverPrompt struct {
	prompt  *protocol.Prompt
	handler PromptHandler
}

func (s *Server) AddPrompt(p *protocol.Prompt, h PromptHandler) {
	if p.Name == "" {
		panic("server: AddPrompt requires a prompt name")
	}
	if h == nil {
		panic(fmt.Sprintf("server: AddPrompt %q requires a handler", p.Name))
	}
	prompt := *p
	s.mu.Lock()
	s.prompts.add(&serverPrompt{prompt: &prompt, handler: h})
	s.promptsDeclared = true
	s.mu.Unlock()
	s.hub.publish(topicPrompts, protocol.NotificationPromptsListChanged, nil)
}

func (s *Server) RemovePrompts(names ...string) {
	s.mu.Lock()
	changed := s.prompts.remove(names...)
	s.mu.Unlock()
	if changed {
		s.hub.publish(topicPrompts, protocol.NotificationPromptsListChanged, nil)
	}
}

func (s *Server) handleListPrompts(ctx context.Context, req *Request) (protocol.Result, error) {
	var p protocol.ListPromptsParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}
	s.mu.RLock()
	items, next, err := paginateList(s.prompts, s.opts.PageSize, p.Cursor)
	s.mu.RUnlock()
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "invalid cursor")
	}
	res := &protocol.ListPromptsResult{Prompts: make([]*protocol.Prompt, 0, len(items)), NextCursor: next}
	for _, sp := range items {
		res.Prompts = append(res.Prompts, sp.prompt)
	}
	return res, nil
}

func (s *Server) handleGetPrompt(ctx context.Context, req *Request) (protocol.Result, error) {
	var p protocol.GetPromptParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}
	s.mu.RLock()
	sp, ok := s.prompts.get(p.Name)
	s.mu.RUnlock()
	if !ok {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "Unknown prompt: %s", p.Name)
	}
	for _, arg := range sp.prompt.Arguments {
		if arg.Required {
			if _, present := p.Arguments[arg.Name]; !present {
				return nil, protocol.Errorf(protocol.CodeInvalidParams,
					"missing required argument %q for prompt %q", arg.Name, p.Name)
			}
		}
	}
	return sp.handler(ctx, &PromptRequest{Request: req, Params: &p})
}
