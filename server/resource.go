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

// FileResourceHandler returns a [ResourceHandler] that serves files from dir.
//
// It protects against path traversal attacks using [filepath.Localize] and [os.OpenRoot].
// If the client provides roots, the requested file must be under at least one root.
//
// dir should be an absolute path. If it is not, it is converted to one using [filepath.Abs].
func FileResourceHandler(dir string) ResourceHandler {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		panic(fmt.Errorf("FileResourceHandler: %w", err))
	}
	return func(ctx context.Context, req *ReadResourceRequest) (*protocol.ReadResourceResult, error) {
		var roots []string
		if rootResult, err := req.Session.ListRoots(ctx); err == nil {
			roots = fileRoots(rootResult.Roots)
		}
		// If ListRoots fails (e.g. client doesn't support roots), proceed without root constraints.

		data, err := readFileResource(req.Params.URI, absDir, roots)
		if err != nil {
			return nil, err
		}

		blob := base64.StdEncoding.EncodeToString(data)
		contents := protocol.ResourceContents{
			URI:  req.Params.URI,
			Blob: blob,
		}
		return &protocol.ReadResourceResult{Contents: []protocol.ResourceContents{contents}}, nil
	}
}

// readFileResource reads from the filesystem at a URI relative to dirFilepath,
// respecting the given root paths.
func readFileResource(rawURI, dirFilepath string, rootFilepaths []string) ([]byte, error) {
	relPath, err := computeURIFilepath(rawURI, dirFilepath, rootFilepaths)
	if err != nil {
		return nil, err
	}

	var data []byte
	err = withFile(dirFilepath, relPath, func(f *os.File) error {
		var err error
		data, err = io.ReadAll(f)
		return err
	})
	if os.IsNotExist(err) {
		return nil, protocol.NewMCPError(protocol.ResourceNotFound, "resource not found", map[string]any{"uri": rawURI})
	}
	return data, err
}

// computeURIFilepath converts a file:// URI to a relative filesystem path under dirFilepath,
// validating against path traversal and root constraints.
func computeURIFilepath(rawURI, dirFilepath string, rootFilepaths []string) (string, error) {
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

	// Localize the URI path: reject "..", absolute paths, etc.
	relPath, err := filepath.Localize(strings.TrimPrefix(uri.Path, "/"))
	if err != nil {
		return "", fmt.Errorf("%q cannot be localized: %w", uri.Path, err)
	}

	// Check against roots if any are configured.
	if len(rootFilepaths) > 0 {
		absPath := filepath.Join(dirFilepath, relPath)
		rootOK := false
		for _, root := range rootFilepaths {
			if rel, err := filepath.Rel(root, absPath); err == nil && filepath.IsLocal(rel) {
				rootOK = true
				break
			}
		}
		if !rootOK {
			return "", fmt.Errorf("URI path %q is not under any root", absPath)
		}
	}

	return relPath, nil
}

// withFile opens a file at join(dir, rel) using [os.OpenRoot] to prevent path traversal,
// then calls f with the opened file.
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

// fileRoots converts protocol Roots to absolute filesystem paths, skipping non-file roots.
func fileRoots(roots []protocol.Root) []string {
	var paths []string
	for _, r := range roots {
		if fp, err := fileRoot(r); err == nil {
			paths = append(paths, fp)
		}
	}
	return paths
}

// fileRoot converts a single Root URI to an absolute filesystem path.
func fileRoot(root protocol.Root) (string, error) {
	u, err := url.Parse(root.URI)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", errors.New("not a file URI")
	}
	if u.Path == "" {
		return "", errors.New("empty path")
	}
	fp := filepath.Clean(filepath.FromSlash(u.Path))
	if !filepath.IsAbs(fp) {
		return "", errors.New("not an absolute path")
	}
	return fp, nil
}
