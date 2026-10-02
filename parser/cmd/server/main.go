// Command server is the parser's Cloud Run service. The Worker's queue
// consumer calls POST /parse with {match_id, demo_key}; the server streams
// the demo from R2, parses it, writes parsed/{match_id}/... back to R2 and
// returns match.json. 422 means the demo is bad; 5xx means retry.
//
// Environment:
//
//	PORT                  listen port (default 8080; Cloud Run sets it)
//	PARSE_SECRET          required; callers send it as X-Parse-Secret
//	R2_ACCOUNT_ID         R2 account; the endpoint is <id>.r2.cloudflarestorage.com
//	R2_ACCESS_KEY_ID      R2 API token, S3 credentials
//	R2_SECRET_ACCESS_KEY
//	R2_BUCKET             bucket holding demos/ and parsed/
//	R2_ENDPOINT           optional S3 endpoint instead of R2's
//	PARSER_STORE_DIR      use this local directory instead of R2 (development)
//	PARSER_STUB_DIR       copy match.json and rounds/*.json from this directory
//	                      instead of parsing (the M2 stub pipeline)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		line, _ := json.Marshal(map[string]string{"severity": "ERROR", "message": err.Error()})
		fmt.Fprintln(os.Stderr, string(line))
		os.Exit(1)
	}
}

// run serves until ctx is done, then shuts down gracefully. Cloud Run sends
// SIGTERM and allows 10 seconds before killing the instance.
func run(ctx context.Context, getenv func(string) string, log io.Writer) error {
	secret := getenv("PARSE_SECRET")
	if secret == "" {
		return errors.New("PARSE_SECRET is required")
	}
	st, err := storeFromEnv(getenv)
	if err != nil {
		return err
	}
	port := getenv("PORT")
	if port == "" {
		port = "8080"
	}

	s := newServer(st, secret, streamParse, getenv("PARSER_STUB_DIR"), log)
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// No write timeout: a parse can take minutes, and Cloud Run enforces
		// the request timeout itself.
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	s.writeLog(map[string]any{
		"severity":       "INFO",
		"message":        "listening",
		"port":           port,
		"parser_version": parserVersion,
		"stub":           s.stubDir != "",
	})

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
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
