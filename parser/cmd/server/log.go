package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/parse"
	"github.com/ChiemekaKalu/demoview/parser/store"
)

// newLogger writes JSON lines keyed severity and message, which Workers Logs
// and Cloud Logging both read. The platform adds the timestamp.
func newLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.TimeKey:
				return slog.Attr{}
			case slog.LevelKey:
				return slog.String("severity", severity(a.Value.Any().(slog.Level)))
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	}))
}

// severity is Cloud Logging's name for a level.
func severity(l slog.Level) string {
	if l == slog.LevelWarn {
		return "WARNING"
	}
	return l.String()
}

// summary is the one line logged per parse request, after the ids the
// request's logger adds. Fields that don't apply are left out.
func (s *server) summary(start time.Time, status int, result string, err error, res runResult) []slog.Attr {
	attrs := append([]slog.Attr{
		slog.String("event", "parse"),
		slog.String("result", result),
		slog.Int("status", status),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		slog.String("parser_version", parse.Version),
	}, nonzero(
		slog.Int64("in_flight", res.inFlight),
		slog.Int64("peak_memory_bytes", res.peakMem),
		slog.String("peak_scope", res.peakScope),
		slog.Bool("stub", s.stubDir != ""),
		slog.Int64("demo_size", res.demoSize),
		slog.Int64("bytes_read", res.parse.BytesRead),
		slog.Int("frames", res.parse.Frames),
		slog.Int("last_tick", int(res.parse.LastTick)),
		slog.Int64("walk_ms", res.parse.Walk.Milliseconds()),
		slog.Int64("build_ms", res.parse.Build.Milliseconds()),
		slog.Int64("r2_get_ms", res.store.get.Milliseconds()),
		slog.Int("put_objects", res.store.puts),
		slog.Int64("put_bytes", res.store.putBytes),
		slog.Int64("put_ms", res.store.put.Milliseconds()),
		slog.Int64("oracle_ms", res.oracleMS),
		slog.Int64("oracle_peak_memory_bytes", res.oraclePeak),
	)...)
	if err != nil {
		// The oracle's stderr is noise on success (demoinfocs warns about
		// unknown grenade models), so it is only kept with an error.
		return append(attrs, nonzero(
			slog.String("error", err.Error()),
			slog.String("stack", res.stack),
			slog.String("oracle_stderr", res.oracleStderr),
		)...)
	}

	m := factsOf(res.matchJSON)
	if m.ParserVersion == parse.Version {
		m.ParserVersion = ""
	}
	return append(attrs, nonzero(
		slog.String("match_source", res.source),
		slog.String("match_parser_version", m.ParserVersion),
		slog.String("map", m.Match.Map),
		slog.String("demo_sha256", m.Match.DemoSHA256),
		slog.Int("players", len(m.Players)),
		slog.Int("rounds", len(m.Rounds)),
		slog.Int("kills", len(m.Kills)),
	)...)
}

// nonzero drops the attrs whose values are zero, like json's omitempty.
func nonzero(attrs ...slog.Attr) []slog.Attr {
	return slices.DeleteFunc(attrs, func(a slog.Attr) bool {
		v := a.Value.Any()
		return v == "" || v == int64(0) || v == false
	})
}

// matchFacts is what the summary line reports of the match.json that
// shipped, whoever wrote it.
type matchFacts struct {
	ParserVersion string `json:"parser_version"`
	Match         struct {
		Map        string `json:"map"`
		DemoSHA256 string `json:"demo_sha256"`
	} `json:"match"`
	Players []struct{} `json:"players"`
	Rounds  []struct{} `json:"rounds"`
	Kills   []struct{} `json:"kills"`
}

func factsOf(matchJSON []byte) matchFacts {
	var m matchFacts
	json.Unmarshal(matchJSON, &m)
	return m
}

// storeStats adds up one parse's store calls.
type storeStats struct {
	get      time.Duration // in Open and Stat, which return at the first byte
	put      time.Duration
	puts     int // objects written
	putBytes int64
}

// timedStore is a Store that adds its calls to stats.
type timedStore struct {
	store.Store
	stats *storeStats
}

func (t timedStore) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	defer addSince(&t.stats.get, time.Now())
	return t.Store.Open(ctx, key)
}

func (t timedStore) Stat(ctx context.Context, key string) (int64, error) {
	defer addSince(&t.stats.get, time.Now())
	return t.Store.Stat(ctx, key)
}

func (t timedStore) Put(ctx context.Context, key string, data []byte) error {
	defer addSince(&t.stats.put, time.Now())
	err := t.Store.Put(ctx, key, data)
	if err == nil {
		t.stats.puts++
		t.stats.putBytes += int64(len(data))
	}
	return err
}

func addSince(d *time.Duration, start time.Time) { *d += time.Since(start) }
