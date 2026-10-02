package store

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

// testStore checks the Store contract. Both implementations run it.
func testStore(t *testing.T, s Store) {
	ctx := context.Background()

	t.Run("put then open and stat", func(t *testing.T) {
		data := []byte("PBDEMS2\x00 demo bytes")
		if err := s.Put(ctx, "demos/abc.dem", data); err != nil {
			t.Fatal(err)
		}
		rc, size, err := s.Open(ctx, "demos/abc.dem")
		if err != nil {
			t.Fatal(err)
		}
		got, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data) || size != int64(len(data)) {
			t.Errorf("Open = %q, size %d; want %q, size %d", got, size, data, len(data))
		}
		if size, err := s.Stat(ctx, "demos/abc.dem"); err != nil || size != int64(len(data)) {
			t.Errorf("Stat = %d, %v; want %d, nil", size, err, len(data))
		}
	})

	t.Run("put replaces", func(t *testing.T) {
		for _, v := range []string{`{"v":1}`, `{"v":2}`} {
			if err := s.Put(ctx, "parsed/abc/match.json", []byte(v)); err != nil {
				t.Fatal(err)
			}
		}
		rc, _, err := s.Open(ctx, "parsed/abc/match.json")
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		if got, _ := io.ReadAll(rc); string(got) != `{"v":2}` {
			t.Errorf("got %s after overwrite, want {\"v\":2}", got)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		if _, _, err := s.Open(ctx, "demos/missing.dem"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Open error = %v, want ErrNotFound", err)
		}
		if _, err := s.Stat(ctx, "demos/missing.dem"); !errors.Is(err, ErrNotFound) {
			t.Errorf("Stat error = %v, want ErrNotFound", err)
		}
	})

	t.Run("invalid keys", func(t *testing.T) {
		for _, key := range []string{"", ".", "/demos/a.dem", "../a.dem", "demos/../../a", "demos//a", `demos\..\a`, "c:/a"} {
			if _, _, err := s.Open(ctx, key); !errors.Is(err, ErrInvalidKey) {
				t.Errorf("Open(%q) error = %v, want ErrInvalidKey", key, err)
			}
			if err := s.Put(ctx, key, nil); !errors.Is(err, ErrInvalidKey) {
				t.Errorf("Put(%q) error = %v, want ErrInvalidKey", key, err)
			}
		}
	})
}

func TestDir(t *testing.T) {
	testStore(t, NewDir(t.TempDir()))
}

func TestDirTreatsDirectoryAsMissing(t *testing.T) {
	d := NewDir(t.TempDir())
	if err := d.Put(context.Background(), "parsed/abc/match.json", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Open(context.Background(), "parsed/abc"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Open(dir) error = %v, want ErrNotFound", err)
	}
}
