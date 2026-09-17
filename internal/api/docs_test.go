package api

import (
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
)

func TestDocsPageIsSelfContained(t *testing.T) {
	srv, _ := newServer(t)

	resp, err := http.Get(srv.URL + "/docs")
	if err != nil {
		t.Fatalf("GET /docs: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	// The lab has to work offline, so the page must reference vendored assets, never a CDN.
	if strings.Contains(page, "//cdn.") || strings.Contains(page, "unpkg.com") {
		t.Fatal("the docs page loads an asset from a CDN; it must serve vendored files")
	}
	for _, want := range []string{"/docs/swagger-ui-bundle.js", "/docs/swagger-ui.css", "/openapi.yaml"} {
		if !strings.Contains(page, want) {
			t.Fatalf("docs page does not reference %s", want)
		}
	}
}

func TestDocsAssetsAreServed(t *testing.T) {
	srv, _ := newServer(t)
	for _, tc := range []struct{ path, contentType string }{
		{"/docs/swagger-ui-bundle.js", "application/javascript"},
		{"/docs/swagger-ui.css", "text/css"},
	} {
		resp, err := http.Get(srv.URL + tc.path)
		if err != nil {
			t.Fatalf("GET %s: %v", tc.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("GET %s = %d with %d bytes", tc.path, resp.StatusCode, len(body))
		}
		if !strings.Contains(resp.Header.Get("Content-Type"), tc.contentType) {
			t.Fatalf("%s content type = %q, want %s", tc.path, resp.Header.Get("Content-Type"), tc.contentType)
		}
	}
}

// A traversal out of the embedded docs directory must not reach anything else.
func TestDocsAssetsRejectTraversal(t *testing.T) {
	srv, _ := newServer(t)
	resp, err := http.Get(srv.URL + "/docs/..%2fopenapi.yaml")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a traversal path was served; docs assets must stay inside the embedded directory")
	}
}

func TestSpecIsServed(t *testing.T) {
	srv, _ := newServer(t)
	resp, err := http.Get(srv.URL + "/openapi.yaml")
	if err != nil {
		t.Fatalf("GET /openapi.yaml: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.HasPrefix(string(body), "openapi: 3") {
		t.Fatalf("spec does not start with an OpenAPI version: %.40q", body)
	}
}

// The spec is hand written, so it can drift from the router. This is the check that stops it:
// every route the server actually serves has to appear in the document people read.
func TestEveryRouteIsDocumented(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	handler := New(st, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	engine, ok := handler.(*gin.Engine)
	if !ok {
		t.Fatalf("handler is %T, want *gin.Engine", handler)
	}

	spec := string(openapiSpec)
	undocumented := []string{}
	for _, r := range engine.Routes() {
		if isDocsRoute(r.Path) {
			continue
		}
		// Gin writes parameters as ":name"; OpenAPI writes them as "{name}".
		path := specPath(r.Path)
		if !strings.Contains(spec, "\n  "+path+":\n") {
			undocumented = append(undocumented, r.Method+" "+r.Path)
		}
	}
	if len(undocumented) > 0 {
		t.Fatalf("routes missing from openapi.yaml: %s", strings.Join(undocumented, ", "))
	}
}

// isDocsRoute reports paths that serve the documentation itself, which the spec does not describe.
func isDocsRoute(path string) bool {
	return path == "/" || path == "/docs" || strings.HasPrefix(path, "/docs/") || path == "/openapi.yaml"
}

func specPath(ginPath string) string {
	parts := strings.Split(ginPath, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") {
			parts[i] = "{" + strings.TrimPrefix(p, ":") + "}"
		}
	}
	return strings.Join(parts, "/")
}
