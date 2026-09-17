// Command platform is every process in the platform: control plane, gateway, model server,
// training worker, evaluation worker, and CLI. One binary keeps the image small and makes the
// whole system readable in one place.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/tihaya-anon/k8s-ai-platform/internal/api"
	"github.com/tihaya-anon/k8s-ai-platform/internal/controller"
	"github.com/tihaya-anon/k8s-ai-platform/internal/ctl"
	"github.com/tihaya-anon/k8s-ai-platform/internal/gateway"
	"github.com/tihaya-anon/k8s-ai-platform/internal/k8s"
	"github.com/tihaya-anon/k8s-ai-platform/internal/serve"
	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
	"github.com/tihaya-anon/k8s-ai-platform/internal/worker"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: platform <control-plane|gateway|serve|train|eval|ctl>")
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "control-plane":
		err = runControlPlane(log)
	case "gateway":
		err = runGateway(log)
	case "serve":
		err = runModelServer(log)
	case "train":
		err = worker.Train(worker.TrainConfigFromEnv(), log)
	case "eval":
		err = worker.Evaluate(worker.EvalConfigFromEnv(), log)
	case "ctl":
		err = ctl.Run(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		log.Error("exited with error", "command", os.Args[1], "err", err)
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// runControlPlane serves the API and runs the reconcile loop in the same process. Splitting them
// is one of the maintainer exercises; keeping them together first makes the data flow obvious.
func runControlPlane(log *slog.Logger) error {
	artifactDir := envOr("ARTIFACT_DIR", "/artifacts")
	st, err := store.Open(envOr("STATE_FILE", artifactDir+"/state/state.json"))
	if err != nil {
		return err
	}

	kube, err := k8s.InCluster()
	if err != nil {
		return err
	}
	cfg := controller.Config{
		Namespace:       kube.Namespace(),
		Image:           envOr("PLATFORM_IMAGE", "ai-platform:dev"),
		ImagePullPolicy: envOr("IMAGE_PULL_POLICY", "IfNotPresent"),
		ServiceAccount:  envOr("WORKLOAD_SERVICE_ACCOUNT", "ai-platform"),
		ArtifactHost:    envOr("ARTIFACT_HOST_PATH", "/artifacts"),
		ArtifactMount:   artifactDir,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ctrl := controller.New(st, kube, cfg, log)
	interval := time.Duration(envInt("RECONCILE_MS", 2000)) * time.Millisecond
	go ctrl.Run(ctx, interval)
	log.Info("controller started", "namespace", cfg.Namespace, "image", cfg.Image, "interval", interval)

	srv := &http.Server{
		Addr:              envOr("LISTEN_ADDR", ":8080"),
		Handler:           api.New(st, artifactDir, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return listen(ctx, srv, log, "control-plane")
}

func runGateway(log *slog.Logger) error {
	cp := envOr("CONTROL_PLANE_URL", "http://ai-platform-control-plane:8080")
	ns := envOr("POD_NAMESPACE", "ai-platform")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	gw := gateway.New(cp, ns, log)
	go gw.WatchRoutes(ctx, time.Duration(envInt("ROUTE_REFRESH_MS", 2000))*time.Millisecond)
	log.Info("gateway started", "controlPlane", cp, "namespace", ns)

	srv := &http.Server{
		Addr:              envOr("LISTEN_ADDR", ":8081"),
		Handler:           gw.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return listen(ctx, srv, log, "gateway")
}

func runModelServer(log *slog.Logger) error {
	ref := os.Getenv("MODEL_REF")
	uri := os.Getenv("ARTIFACT_URI")
	if ref == "" || uri == "" {
		return errors.New("MODEL_REF and ARTIFACT_URI are required")
	}
	s, err := serve.New(ref, uri, log)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	log.Info("model server started", "modelRef", ref, "artifact", uri)

	srv := &http.Server{
		Addr:              envOr("LISTEN_ADDR", ":9000"),
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	return listen(ctx, srv, log, "model-server")
}

// listen serves until the context is cancelled, then drains in-flight requests.
func listen(ctx context.Context, srv *http.Server, log *slog.Logger, name string) error {
	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "component", name, "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log.Info("shutting down", "component", name)
		return srv.Shutdown(shutdownCtx)
	}
}
