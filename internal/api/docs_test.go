package api

import (
	"flag"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
)

var updateSpec = flag.Bool("update", false, "rewrite openapi.yaml from the registered operations")

const specFile = "openapi.yaml"

// The documentation page is Huma's, rendered from the generated spec. The check is that it is
// wired to this API's document rather than to a default, since a docs page pointed at the wrong
// URL still renders and still looks fine.
func TestDocsPageIsServed(t *testing.T) {
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
	if !strings.Contains(page, "/openapi.json") {
		t.Fatalf("docs page does not load this API's spec:\n%.400s", page)
	}
	// The page is only useful in this lab if a reader can fire the calls they are reading about.
	if !strings.Contains(page, "tryItOutEnabled") {
		t.Fatal("docs page does not enable Try it out")
	}
}

// The root is the address people type; it has to land somewhere useful.
func TestRootRedirectsToDocs(t *testing.T) {
	srv, _ := newServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/docs" {
		t.Fatalf("GET / = %d to %q, want 302 to /docs", resp.StatusCode, resp.Header.Get("Location"))
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
	// Key order comes from the generator, so the version is looked for rather than assumed first.
	if !strings.Contains(string(body), "openapi: 3.1.0") {
		t.Fatalf("served document declares no OpenAPI version:\n%.200s", body)
	}
}

// The spec is generated from the handler types, so it cannot describe an endpoint the server does
// not serve. What it can do is change without anyone noticing, because nobody reads a document
// that regenerates itself. The snapshot in openapi.yaml exists so a contract change arrives as a
// reviewable diff; this test is what keeps the snapshot honest.
//
// Run `make openapi` (or `go test ./internal/api -update`) after changing an endpoint.
func TestSpecSnapshotIsCurrent(t *testing.T) {
	generated, err := newTestServer(t).Spec()
	if err != nil {
		t.Fatalf("generate spec: %v", err)
	}
	if *updateSpec {
		if err := os.WriteFile(specFile, generated, 0o644); err != nil {
			t.Fatalf("write %s: %v", specFile, err)
		}
		t.Logf("wrote %s (%d bytes)", specFile, len(generated))
		return
	}
	committed, err := os.ReadFile(specFile)
	if err != nil {
		t.Fatalf("read %s: %v", specFile, err)
	}
	if string(committed) != string(generated) {
		t.Fatalf("%s is stale: it no longer matches the registered operations.\n"+
			"Run `make openapi` to refresh it, then review the diff.\n%s",
			specFile, firstDifference(string(committed), string(generated)))
	}
}

// Every operation carries prose a stranger can act on. Huma guarantees the document describes
// exactly what is served; nothing guarantees it says anything useful, so that part is checked here.
func TestEveryOperationIsDescribed(t *testing.T) {
	_, api := newTestServer(t).build()
	for path, item := range api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			where := op.Method + " " + path
			if op.OperationID == "" {
				t.Errorf("%s has no operationId; clients generate method names from it", where)
			}
			if op.Summary == "" {
				t.Errorf("%s has no summary; it renders as a bare path on the docs page", where)
			}
			if len(op.Tags) == 0 {
				t.Errorf("%s has no tag, so the docs page files it under no heading", where)
			}
		}
	}
}

// newTestServer builds a Server over a throwaway store, for tests that only need the shape of the
// API rather than its data.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	return New(st, t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// firstDifference reports the first differing line, so a stale snapshot points at what changed
// instead of dumping the whole document.
func firstDifference(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		x, y := "", ""
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			return "first difference at line " + strconv.Itoa(i+1) + ":\n  committed: " + x + "\n  generated: " + y
		}
	}
	return ""
}
