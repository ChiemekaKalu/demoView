package roundfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestName(t *testing.T) {
	for n, want := range map[int]string{1: "01.json", 12: "12.json", 105: "105.json"} {
		if got := Name(n); got != want {
			t.Errorf("Name(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestWrite(t *testing.T) {
	f := File{
		SchemaVersion: 1, Round: 3, Hz: 64, StartTick: 1000, Frames: 2,
		Players:  []Player{{Idx: 4, X: []int16{-5, -6}, Y: []int16{7, 8}, Yaw: []int16{0, 359}, HP: []int16{100, 0}}},
		Grenades: []Grenade{{Type: Smoke, Thrower: 4, ThrowFrame: 0, DetonateFrame: 1, EndFrame: 1, Path: [][2]int16{{1, 2}, {3, 4}}}},
		Events:   []Event{{Frame: 1, Type: EventKill, Kill: 9}},
	}
	var b bytes.Buffer
	if err := Write(&b, f); err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"round":3,"hz":64,"start_tick":1000,"frames":2,` +
		`"players":[{"idx":4,"x":[-5,-6],"y":[7,8],"yaw":[0,359],"hp":[100,0]}],` +
		`"grenades":[{"type":"smoke","thrower":4,"throw_frame":0,"detonate_frame":1,"end_frame":1,"path":[[1,2],[3,4]]}],` +
		`"events":[{"frame":1,"type":"kill","kill":9}]}` + "\n"
	if b.String() != want {
		t.Errorf("got\n%s\nwant\n%s", b.String(), want)
	}

	b.Reset()
	if err := Write(&b, File{Round: 1, Frames: 1}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b.Bytes(), []byte(`"players":[],"grenades":[],"events":[]`)) {
		t.Errorf("nil lists: %s", b.Bytes())
	}
}

func TestWriteFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	if err := WriteFile(dir, File{Round: 7, Frames: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rounds", "07.json")); err != nil {
		t.Error(err)
	}
}
