// Package k8s is a deliberately small Kubernetes client built on net/http. It speaks the REST API
// directly so that the mapping from platform object to Kubernetes object stays visible instead of
// hiding behind a generated clientset.
package k8s

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const saDir = "/var/run/secrets/kubernetes.io/serviceaccount"

// ErrNotFound maps HTTP 404 from the API server.
var ErrNotFound = errors.New("kubernetes object not found")

// Client talks to one API server with one identity.
type Client struct {
	host      string
	token     string
	namespace string
	http      *http.Client
}

// InCluster builds a client from the mounted service account. KUBE_API_HOST and KUBE_TOKEN can
// override it, which is how the control plane is run outside the cluster during development.
func InCluster() (*Client, error) {
	host := os.Getenv("KUBE_API_HOST")
	if host == "" {
		h, p := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
		if h == "" {
			return nil, errors.New("not running in a cluster: set KUBE_API_HOST or run in a pod")
		}
		if p == "" {
			p = "443"
		}
		host = "https://" + net(h, p)
	}

	token := os.Getenv("KUBE_TOKEN")
	if token == "" {
		b, err := os.ReadFile(saDir + "/token")
		if err != nil {
			return nil, fmt.Errorf("read service account token: %w", err)
		}
		token = strings.TrimSpace(string(b))
	}

	ns := os.Getenv("POD_NAMESPACE")
	if ns == "" {
		if b, err := os.ReadFile(saDir + "/namespace"); err == nil {
			ns = strings.TrimSpace(string(b))
		} else {
			ns = "default"
		}
	}

	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if ca, err := os.ReadFile(saDir + "/ca.crt"); err == nil {
		pool := x509.NewCertPool()
		if pool.AppendCertsFromPEM(ca) {
			tlsCfg.RootCAs = pool
		}
	} else if os.Getenv("KUBE_INSECURE") == "true" {
		tlsCfg.InsecureSkipVerify = true
	}

	return &Client{
		host:      strings.TrimSuffix(host, "/"),
		token:     token,
		namespace: ns,
		http: &http.Client{
			Timeout:   20 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

func net(host, port string) string {
	if strings.Contains(host, ":") { // IPv6
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// Namespace is the namespace the control plane manages.
func (c *Client) Namespace() string { return c.namespace }

func (c *Client) do(ctx context.Context, method, path string, body any, contentType string) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrNotFound
	case resp.StatusCode >= 300:
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, truncate(string(out), 400))
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Object is an untyped Kubernetes object. The control plane only reads a few fields, so a map
// keeps the client small and the manifests readable at the call site.
type Object map[string]any

// Apply server-side applies an object, which makes the reconcile loop naturally idempotent.
func (c *Client) Apply(ctx context.Context, apiPath, name string, obj Object) error {
	q := url.Values{"fieldManager": {"ai-platform"}, "force": {"true"}}
	_, err := c.do(ctx, http.MethodPatch, apiPath+"/"+name+"?"+q.Encode(), obj, "application/apply-patch+yaml")
	return err
}

// Get fetches one object.
func (c *Client) Get(ctx context.Context, apiPath, name string) (Object, error) {
	b, err := c.do(ctx, http.MethodGet, apiPath+"/"+name, nil, "")
	if err != nil {
		return nil, err
	}
	var o Object
	if err := json.Unmarshal(b, &o); err != nil {
		return nil, err
	}
	return o, nil
}

// Delete removes an object and its dependents.
func (c *Client) Delete(ctx context.Context, apiPath, name string) error {
	body := Object{"apiVersion": "meta.k8s.io/v1", "kind": "DeleteOptions", "propagationPolicy": "Background"}
	_, err := c.do(ctx, http.MethodDelete, apiPath+"/"+name, body, "")
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

// List returns the items of a collection, optionally filtered by label selector.
func (c *Client) List(ctx context.Context, apiPath, labelSelector string) ([]Object, error) {
	p := apiPath
	if labelSelector != "" {
		p += "?labelSelector=" + url.QueryEscape(labelSelector)
	}
	b, err := c.do(ctx, http.MethodGet, p, nil, "")
	if err != nil {
		return nil, err
	}
	var out struct {
		Items []Object `json:"items"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// PodLogs returns the tail of a pod's logs, which is what incident triage reads first.
func (c *Client) PodLogs(ctx context.Context, name string, tailLines int) (string, error) {
	q := url.Values{"tailLines": {fmt.Sprint(tailLines)}}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log?%s", c.namespace, name, q.Encode())
	b, err := c.do(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Resource paths for the object kinds the platform creates.

func (c *Client) JobsPath() string {
	return fmt.Sprintf("/apis/batch/v1/namespaces/%s/jobs", c.namespace)
}

func (c *Client) DeploymentsPath() string {
	return fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments", c.namespace)
}

func (c *Client) ServicesPath() string {
	return fmt.Sprintf("/api/v1/namespaces/%s/services", c.namespace)
}

func (c *Client) PodsPath() string {
	return fmt.Sprintf("/api/v1/namespaces/%s/pods", c.namespace)
}

// Nested reads a path of string keys out of an untyped object. It accepts both the maps that come
// back from the API server and the Object literals the controller builds.
func Nested(o Object, keys ...string) (any, bool) {
	var cur any = o
	for _, k := range keys {
		m, ok := asMap(cur)
		if !ok {
			return nil, false
		}
		cur, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case Object:
		return m, true
	case map[string]any:
		return m, true
	}
	return nil, false
}

// NestedInt reads an integer field, tolerating JSON's float64 decoding.
func NestedInt(o Object, keys ...string) int {
	v, ok := Nested(o, keys...)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// NestedString reads a string field.
func NestedString(o Object, keys ...string) string {
	v, ok := Nested(o, keys...)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
