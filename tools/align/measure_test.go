package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ChiemekaKalu/demoview/tools/align/radar"
)

func write(t *testing.T, path, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMeasure uses a 4x4 radar whose left half is opaque, placed so that
// game (x, y) is pixel (x, 4-y).
func TestMeasure(t *testing.T) {
	a := image.NewAlpha(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		a.SetAlpha(0, y, color.Alpha{255})
		a.SetAlpha(1, y, color.Alpha{255})
	}
	r := &radar.Radar{Info: radar.Info{Name: "de_test", PosX: 0, PosY: 4, Scale: 1}, Alpha: a}

	dir := filepath.Join(t.TempDir(), "demo")
	write(t, filepath.Join(dir, "match.json"), `{"match": {"map": "de_test"}}`)
	write(t, filepath.Join(dir, "rounds", "01.json"), `{"round": 1, "players": [
		{"idx": 0, "x": [0, 1, 3, 3, 40], "y": [4, 1, 1, 1, 1], "hp": [100, 100, 100, 0, 50]},
		{"idx": 1, "x": [3, 3], "y": [2, 2], "hp": [10, 10]}]}`)
	write(t, filepath.Join(dir, "rounds", "02.json"), `{"round": 2, "players": [
		{"idx": 0, "x": [3], "y": [2], "hp": [100]}]}`)

	files, err := roundFiles([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := measure(r, files, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := &report{
		Map: "de_test", CellPx: cellPx,
		tally: tally{7, 2, 28.571},
		Demos: []*demo{{Name: "demo", tally: tally{7, 2, 28.571}, Rounds: []round{
			{1, tally{6, 2, 33.333}},
			{2, tally{1, 0, 0}},
		}}},
		OffRadar: []cluster{{Frames: 4, Rounds: 2, X: 3, Y: 2, PX: 3, PY: 2}},
	}
	if !reflect.DeepEqual(rep, want) {
		t.Errorf("measure =\n%+v\nwant\n%+v", rep, want)
	}

	other := filepath.Join(t.TempDir(), "other")
	write(t, filepath.Join(other, "match.json"), `{"match": {"map": "de_other"}}`)
	write(t, filepath.Join(other, "rounds", "01.json"), `{"round": 1, "players": []}`)
	if _, err := measure(r, []string{filepath.Join(other, "rounds", "01.json")}, 0, 1); err == nil {
		t.Error("a round file from another map measured without error")
	}
}
