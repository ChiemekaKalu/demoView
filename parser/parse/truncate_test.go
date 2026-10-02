package parse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
	"time"

	"github.com/ChiemekaKalu/demoview/parser/demo"
)

// cuts is how many evenly spaced offsets TestTruncated cuts each demo at.
const cuts = 1000

// TestTruncated cuts each public fixture at offsets i/1000 of its length,
// for i from 0 to 999, and checks that every cut is a bad demo: never a
// panic, a success or an I/O error. Cuts land mid-frame or between frames,
// before Stop. It skips under -short, and without the fixtures.
func TestTruncated(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	for _, name := range []string{"faceit_nuke_2026.dem", "valve_mm_mirage_2024.dem"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "testdata", "demos", name)
			info, err := os.Stat(path)
			if err != nil {
				t.Skip("not in testdata/demos")
			}
			start := time.Now()
			offsets := make(chan int64)
			var wg sync.WaitGroup
			for range runtime.GOMAXPROCS(0) {
				wg.Go(func() {
					for off := range offsets {
						if err := parsePrefix(path, off); !errors.Is(err, demo.ErrBadDemo) {
							t.Errorf("cut at %d of %d bytes: error %v, want ErrBadDemo", off, info.Size(), err)
						}
					}
				})
			}
			for i := range int64(cuts) {
				offsets <- info.Size() * i / cuts
			}
			close(offsets)
			wg.Wait()
			t.Logf("%d cuts of %d bytes in %v", cuts, info.Size(), time.Since(start).Round(time.Millisecond))
		})
	}
}

// parsePrefix runs Run on the first n bytes of the file at path, turning a
// panic into an error.
func parsePrefix(path string, n int64) (err error) {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	_, err = Run(context.Background(), io.LimitReader(f, n))
	return err
}
