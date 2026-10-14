// Package artifact reads metadata about a dataset artifact without copying it
// into the cluster. The platform records what an artifact looks like; moving its
// bytes is the training pod's job.
package artifact

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
)

// Metadata is what can be learned about an artifact without downloading it into
// the controller. It mirrors the claims a user may put in DatasetSpec, so the
// reconciler can compare the two.
type Metadata struct {
	Rows     int
	Checksum string
}

// Resolver reads artifact metadata at a URI.
//
// This is an interface because envtest runs an API server but no network: the
// suite injects a fake, the manager injects a real store. A reconciler that
// dials object storage directly cannot be tested without one.
type Resolver interface {
	Resolve(ctx context.Context, uri string) (Metadata, error)
}

// FileResolver resolves file:// URIs by reading the artifact from disk.
type FileResolver struct{}

// ensure interface implementation
var _ Resolver = (*FileResolver)(nil)

// Resolve counts newline-terminated rows and hashes the content.
func (FileResolver) Resolve(ctx context.Context, uri string) (Metadata, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return Metadata{}, fmt.Errorf("parse uri: %w", err)
	}
	if parsed.Scheme != "file" {
		return Metadata{}, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	file, err := os.Open(parsed.Path)
	if err != nil {
		return Metadata{}, fmt.Errorf("open artifact: %w", err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	rows := 0
	scanner := bufio.NewScanner(io.TeeReader(file, hash))
	for scanner.Scan() {
		// Cancellation matters here: a large artifact must not keep a worker busy
		// after the manager has started shutting down.
		if err := ctx.Err(); err != nil {
			return Metadata{}, err
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		return Metadata{}, fmt.Errorf("read artifact: %w", err)
	}

	return Metadata{Rows: rows, Checksum: hex.EncodeToString(hash.Sum(nil))}, nil
}

// FakeResolver is an in-memory Resolver for tests.
type FakeResolver struct {
	Entries map[string]Metadata
	Err     error
}

// ensure interface implementation
var _ Resolver = (*FakeResolver)(nil)

// Resolve returns the configured error, or the entry registered for uri.
func (f *FakeResolver) Resolve(_ context.Context, uri string) (Metadata, error) {
	if f.Err != nil {
		return Metadata{}, f.Err
	}
	found, ok := f.Entries[uri]
	if !ok {
		return Metadata{}, fmt.Errorf("no artifact at %q", uri)
	}
	return found, nil
}
