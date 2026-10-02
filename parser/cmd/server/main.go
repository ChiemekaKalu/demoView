// Command server is the parser's HTTP service, run as a container on
// Cloudflare Containers or Cloud Run. The Worker's queue consumer calls POST
// /parse with {match_id, demo_key}; the server streams the demo from R2,
// parses it, writes parsed/{match_id}/... back to R2 and returns match.json.
// 422 means the demo is bad; 5xx means retry.
//
// It logs JSON lines to stdout, with one summary line per parse.
//
// Environment:
//
//	PORT                  listen port (default 8080)
//	PARSE_SECRET          required; callers send it as X-Parse-Secret
//	R2_ACCOUNT_ID         R2 account; the endpoint is <id>.r2.cloudflarestorage.com
//	R2_ACCESS_KEY_ID      R2 API token, S3 credentials
//	R2_SECRET_ACCESS_KEY
//	R2_BUCKET             bucket holding demos/ and parsed/
//	R2_ENDPOINT           optional S3 endpoint instead of R2's
//	PARSER_STORE_DIR      use this local directory instead of R2 (development)
//	PARSER_STUB_DIR       copy match.json and rounds/*.json from this directory
//	                      instead of parsing (the M2 stub pipeline)
//	PARSER_ROUNDS         "oracle" to write round files with tools/oracle, the
//	                      v1 setting; unset writes match.json only
//	PARSER_ORACLE_BIN     the oracle binary (default "oracle" on PATH; the
//	                      image installs it at /usr/local/bin/oracle)
//	PARSER_BUILD          the build id the Worker reparses by (default "dev");
//	                      written to each manifest as parsed_by
package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

// drainTimeout bounds the wait for in-flight parses at shutdown. Cloudflare
// Containers sends SIGKILL 15 minutes after SIGTERM. Cloud Run kills sooner,
// at its own grace period, which only cuts the drain short.
const drainTimeout = 14 * time.Minute

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, stop) // a second signal kills without waiting for the drain
	logger := newLogger(os.Stdout)
	if err := run(ctx, os.Getenv, logger); err != nil {
		logger.Error("exiting", "error", err)
		os.Exit(1)
	}
}

// run serves until ctx is done, then waits up to drainTimeout for in-flight
// parses to finish.
func run(ctx context.Context, getenv func(string) string, logger *slog.Logger) error {
	secret := getenv("PARSE_SECRET")
	if secret == "" {
		return errors.New("PARSE_SECRET is required")
	}
	st, err := storeFromEnv(getenv)
	if err != nil {
		return err
	}
	s := newServer(st, secret, parse.RunStats, getenv("PARSER_STUB_DIR"), logger)
	s.build = cmp.Or(getenv("PARSER_BUILD"), s.build)
	if s.oracle, err = oracleFromEnv(getenv); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", ":"+cmp.Or(getenv("PORT"), "8080"))
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           s.routes(),
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: a parse can take minutes, and the platform
		// enforces its own request timeout.
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	logger.Info("listening",
		"port", ln.Addr().(*net.TCPAddr).Port,
		"parser_version", parse.Version,
		"build", s.build,
		"store", storeKind(st),
		"rounds", cmp.Or(getenv("PARSER_ROUNDS"), "none"),
		"stub", s.stubDir != "",
		"gomaxprocs", runtime.GOMAXPROCS(0),
		"peak_reset", s.resetPeak(),
	)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutdown", "in_flight", s.inFlight.Load())
	drainCtx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		logger.Error("shutdown timed out, abandoning in-flight parses", "in_flight", s.inFlight.Load(), "error", err)
	}
	logger.Info("stopped")
	return nil
}

// storeFromEnv uses PARSER_STORE_DIR when set and R2 otherwise.
func storeFromEnv(getenv func(string) string) (store.Store, error) {
	if dir := getenv("PARSER_STORE_DIR"); dir != "" {
		return store.NewDir(dir), nil
	}
	cfg, err := store.R2ConfigFromEnv(getenv)
	if err != nil {
		return nil, fmt.Errorf("%w (or set PARSER_STORE_DIR for a local directory)", err)
	}
	return store.NewR2(cfg), nil
}

// storeKind names the store for the startup line, which never logs the
// endpoint or credentials.
func storeKind(st store.Store) string {
	if _, ok := st.(*store.Dir); ok {
		return "dir"
	}
	return "r2"
}

// oracleFromEnv returns the round file producer PARSER_ROUNDS asks for: nil
// when unset, or the oracle, whose binary must exist so a misconfigured
// image fails at startup instead of on every parse.
func oracleFromEnv(getenv func(string) string) (*oracle, error) {
	switch mode := getenv("PARSER_ROUNDS"); mode {
	case "":
		return nil, nil
	case "oracle":
		bin := getenv("PARSER_ORACLE_BIN")
		if bin == "" {
			bin = "oracle"
		}
		path, err := exec.LookPath(bin)
		if err != nil {
			return nil, fmt.Errorf("PARSER_ROUNDS=oracle: %w", err)
		}
		return &oracle{argv: []string{path}}, nil
	default:
		return nil, fmt.Errorf("PARSER_ROUNDS=%q: want \"oracle\" or unset", mode)
	}
}
