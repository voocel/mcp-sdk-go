package server

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// ResourceHandler resolves resources/read. Return *protocol.ReadResourceResult
// or an MRTR interim result via protocol.RequireInput.
type ResourceHandler func(ctx context.Context, req *ResourceRequest) (protocol.ResourceResponse, error)

type serverResource struct {
	resource *protocol.Resource
	handler  ResourceHandler
}

type serverResourceTemplate struct {
	template *protocol.ResourceTemplate
	handler  ResourceHandler
}

func (s *Server) AddResource(r *protocol.Resource, h ResourceHandler) {
	if r.URI == "" {
		panic("server: AddResource requires a URI")
	}
	if h == nil {
		panic(fmt.Sprintf("server: AddResource %q requires a handler", r.URI))
	}
	resource := *r
	s.mu.Lock()
	s.resources.add(&serverResource{resource: &resource, handler: h})
	s.resourcesDeclared = true
	s.mu.Unlock()
	s.hub.publish(topicResources, protocol.NotificationResourcesListChanged, nil)
}

func (s *Server) RemoveResources(uris ...string) {
	s.mu.Lock()
	changed := s.resources.remove(uris...)
	s.mu.Unlock()
	if changed {
		s.hub.publish(topicResources, protocol.NotificationResourcesListChanged, nil)
	}
}

// AddResourceTemplate registers a URI template. Templates support single-
// segment {var} placeholders (e.g. "log://app/{date}"); matched variables are
// exposed to the handler via req.TemplateVars().
func (s *Server) AddResourceTemplate(t *protocol.ResourceTemplate, h ResourceHandler) {
	if t.URITemplate == "" {
		panic("server: AddResourceTemplate requires a URI template")
	}
	if h == nil {
		panic(fmt.Sprintf("server: AddResourceTemplate %q requires a handler", t.URITemplate))
	}
	if err := validateURITemplate(t.URITemplate); err != nil {
		panic(fmt.Sprintf("server: AddResourceTemplate %q: %v", t.URITemplate, err))
	}
	template := *t
	s.mu.Lock()
	s.resourceTemplates.add(&serverResourceTemplate{template: &template, handler: h})
	s.resourcesDeclared = true
	s.mu.Unlock()
	s.hub.publish(topicResources, protocol.NotificationResourcesListChanged, nil)
}

func (s *Server) RemoveResourceTemplates(uriTemplates ...string) {
	s.mu.Lock()
	changed := s.resourceTemplates.remove(uriTemplates...)
	s.mu.Unlock()
	if changed {
		s.hub.publish(topicResources, protocol.NotificationResourcesListChanged, nil)
	}
}

// ResourceUpdated publishes notifications/resources/updated to subscription
// streams that subscribed to uri.
func (s *Server) ResourceUpdated(uri string) {
	s.hub.publish(topicResource(uri), protocol.NotificationResourcesUpdated,
		protocol.ResourceUpdatedParams{URI: uri})
}

func (s *Server) handleListResources(ctx context.Context, req *Request) (protocol.Result, error) {
	var p protocol.ListResourcesParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}
	s.mu.RLock()
	items, next, err := paginateList(s.resources, s.opts.PageSize, p.Cursor)
	s.mu.RUnlock()
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "invalid cursor")
	}
	res := &protocol.ListResourcesResult{Resources: make([]*protocol.Resource, 0, len(items)), NextCursor: next}
	for _, sr := range items {
		res.Resources = append(res.Resources, sr.resource)
	}
	return res, nil
}

func (s *Server) handleListResourceTemplates(ctx context.Context, req *Request) (protocol.Result, error) {
	var p protocol.ListResourceTemplatesParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}
	s.mu.RLock()
	items, next, err := paginateList(s.resourceTemplates, s.opts.PageSize, p.Cursor)
	s.mu.RUnlock()
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "invalid cursor")
	}
	res := &protocol.ListResourceTemplatesResult{
		ResourceTemplates: make([]*protocol.ResourceTemplate, 0, len(items)),
		NextCursor:        next,
	}
	for _, st := range items {
		res.ResourceTemplates = append(res.ResourceTemplates, st.template)
	}
	return res, nil
}

func (s *Server) handleReadResource(ctx context.Context, req *Request) (protocol.Result, error) {
	var p protocol.ReadResourceParams
	if err := unmarshalParams(req.rawParams, &p); err != nil {
		return nil, err
	}
	s.mu.RLock()
	sr, exact := s.resources.get(p.URI)
	var (
		tmplHandler ResourceHandler
		tmplVars    map[string]string
	)
	if !exact {
		for st := range s.resourceTemplates.all() {
			if vars, ok := matchURITemplate(st.template.URITemplate, p.URI); ok {
				tmplHandler = st.handler
				tmplVars = vars
				break
			}
		}
	}
	s.mu.RUnlock()

	rreq := &ResourceRequest{Request: req, Params: &p}
	switch {
	case exact:
		return sr.handler(ctx, rreq)
	case tmplHandler != nil:
		req.templateVars = tmplVars
		return tmplHandler(ctx, rreq)
	}
	return nil, protocol.ResourceNotFoundError(p.URI)
}

// validateURITemplate checks brace balance and variable names.
func validateURITemplate(tmpl string) error {
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '{' {
			continue
		}
		end := strings.IndexByte(tmpl[i:], '}')
		if end <= 1 {
			return errors.New("unbalanced or empty {variable}")
		}
		i += end
	}
	if strings.IndexByte(tmpl, '}') != -1 && strings.IndexByte(tmpl, '{') == -1 {
		return errors.New("unbalanced '}'")
	}
	return nil
}

// matchURITemplate matches uri against a template with single-segment {var}
// placeholders. A variable matches one or more characters up to the next
// literal character in the template, never crossing '/'.
func matchURITemplate(tmpl, uri string) (map[string]string, bool) {
	vars := make(map[string]string)
	ti, ui := 0, 0
	for ti < len(tmpl) {
		if tmpl[ti] == '{' {
			end := strings.IndexByte(tmpl[ti:], '}')
			if end < 0 {
				return nil, false
			}
			name := tmpl[ti+1 : ti+end]
			ti += end + 1
			// The variable value runs until the next literal char or '/'.
			stop := len(uri)
			if ti < len(tmpl) {
				rel := strings.IndexByte(uri[ui:], tmpl[ti])
				if rel < 0 {
					return nil, false
				}
				stop = ui + rel
			}
			if slash := strings.IndexByte(uri[ui:], '/'); slash >= 0 && ui+slash < stop {
				return nil, false
			}
			if stop == ui {
				return nil, false // empty match
			}
			vars[name] = uri[ui:stop]
			ui = stop
			continue
		}
		if ui >= len(uri) || uri[ui] != tmpl[ti] {
			return nil, false
		}
		ti++
		ui++
	}
	if ui != len(uri) {
		return nil, false
	}
	return vars, true
}

// FileResourceHandler returns a ResourceHandler serving files under dir as
// base64 blobs. It protects against path traversal with filepath.Localize and
// os.OpenRoot.
func FileResourceHandler(dir string) ResourceHandler {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		panic(fmt.Errorf("server: FileResourceHandler: %w", err))
	}
	return func(ctx context.Context, req *ResourceRequest) (protocol.ResourceResponse, error) {
		data, err := readFileResource(req.Params.URI, absDir)
		if err != nil {
			return nil, err
		}
		contents := protocol.ResourceContents{
			URI:  req.Params.URI,
			Blob: base64.StdEncoding.EncodeToString(data),
		}
		return protocol.NewReadResourceResult(contents), nil
	}
}

func readFileResource(rawURI, dirFilepath string) ([]byte, error) {
	relPath, err := computeURIFilepath(rawURI)
	if err != nil {
		return nil, protocol.Errorf(protocol.CodeInvalidParams, "invalid resource URI %q: %v", rawURI, err)
	}
	var data []byte
	err = withFile(dirFilepath, relPath, func(f *os.File) error {
		var err error
		data, err = io.ReadAll(f)
		return err
	})
	if os.IsNotExist(err) {
		return nil, protocol.ResourceNotFoundError(rawURI)
	}
	return data, err
}

// computeURIFilepath converts a file:// URI into a relative filesystem path,
// rejecting traversal attempts.
func computeURIFilepath(rawURI string) (string, error) {
	uri, err := url.Parse(rawURI)
	if err != nil {
		return "", err
	}
	if uri.Scheme != "file" {
		return "", fmt.Errorf("URI is not a file: %s", uri)
	}
	if uri.Path == "" {
		return "", errors.New("empty path")
	}
	relPath, err := filepath.Localize(strings.TrimPrefix(uri.Path, "/"))
	if err != nil {
		return "", fmt.Errorf("%q cannot be localized: %w", uri.Path, err)
	}
	return relPath, nil
}

// withFile opens join(dir, rel) via os.OpenRoot to prevent path traversal.
func withFile(dir, rel string, f func(*os.File) error) (err error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()

	file, err := r.Open(rel)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	return f(file)
}
