package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/ChiemekaKalu/demoview/tools/align/radar"
)

// cellPx is the side of the pixel cells that off-radar frames are binned in.
const cellPx = 16

type tally struct {
	Frames int     `json:"frames"` // alive player frames
	On     int     `json:"on"`     // of those, frames on the radar
	Pct    float64 `json:"pct"`
}

func (t *tally) count(frames, on int) {
	t.Frames += frames
	t.On += on
	if t.Frames > 0 {
		t.Pct = math.Round(1e5*float64(t.On)/float64(t.Frames)) / 1e3
	}
}

type report struct {
	Map    string `json:"map"`
	Alpha  uint8  `json:"alpha_above"`
	CellPx int    `json:"cell_px"`
	tally
	Demos    []*demo   `json:"demos"`
	OffRadar []cluster `json:"off_radar"` // the busiest off-radar cells
}

type demo struct {
	Name string `json:"name"`
	tally
	Rounds []round `json:"rounds"`
}

type round struct {
	N int `json:"round"`
	tally
}

// cluster is an off-radar cell: its frame count, the number of rounds that
// reach it, and the mean position of those frames in game and pixel space.
type cluster struct {
	Frames int `json:"frames"`
	Rounds int `json:"rounds"`
	X      int `json:"x"`
	Y      int `json:"y"`
	PX     int `json:"px"`
	PY     int `json:"py"`
}

type cell struct {
	frames, rounds, last int
	sx, sy               float64
}

type roundFile struct {
	Round   int `json:"round"`
	Players []struct {
		Idx int   `json:"idx"`
		X   []int `json:"x"`
		Y   []int `json:"y"`
		HP  []int `json:"hp"`
	} `json:"players"`
}

// roundFiles expands parse dirs into their round files.
func roundFiles(args []string) ([]string, error) {
	var files []string
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			files = append(files, a)
			continue
		}
		m, _ := filepath.Glob(filepath.Join(a, "rounds", "*.json"))
		if len(m) == 0 {
			return nil, fmt.Errorf("%s: no rounds/*.json", a)
		}
		files = append(files, m...)
	}
	return files, nil
}

// measure counts alive player frames on and off the radar. A demo is the
// directory above a round file's rounds/.
func measure(r *radar.Radar, files []string, above uint8, top int) (*report, error) {
	rep := &report{Map: r.Name, Alpha: above, CellPx: cellPx}
	demos := map[string]*demo{}
	cells := map[[2]int]*cell{}
	for n, f := range files {
		dir := filepath.Dir(filepath.Dir(f))
		d := demos[dir]
		if d == nil {
			if err := checkMap(dir, r.Name); err != nil {
				return nil, err
			}
			d = &demo{Name: filepath.Base(dir)}
			demos[dir] = d
			rep.Demos = append(rep.Demos, d)
		}
		var rf roundFile
		if err := readJSON(f, &rf); err != nil {
			return nil, err
		}
		frames, on := 0, 0
		for _, p := range rf.Players {
			if len(p.X) != len(p.HP) || len(p.Y) != len(p.HP) {
				return nil, fmt.Errorf("%s: player %d: column lengths differ", f, p.Idx)
			}
			for i, hp := range p.HP {
				if hp <= 0 {
					continue
				}
				frames++
				x, y := float64(p.X[i]), float64(p.Y[i])
				px, py := r.Pixel(x, y)
				if r.Opaque(px, py, above) {
					on++
					continue
				}
				k := [2]int{int(math.Floor(px / cellPx)), int(math.Floor(py / cellPx))}
				c := cells[k]
				if c == nil {
					c = &cell{}
					cells[k] = c
				}
				c.frames++
				c.sx += x
				c.sy += y
				if c.last != n+1 {
					c.last = n + 1
					c.rounds++
				}
			}
		}
		rd := round{N: rf.Round}
		rd.count(frames, on)
		d.Rounds = append(d.Rounds, rd)
		d.count(frames, on)
		rep.count(frames, on)
	}

	rep.OffRadar = []cluster{}
	for _, c := range cells {
		x, y := c.sx/float64(c.frames), c.sy/float64(c.frames)
		px, py := r.Pixel(x, y)
		rep.OffRadar = append(rep.OffRadar, cluster{c.frames, c.rounds, rnd(x), rnd(y), rnd(px), rnd(py)})
	}
	slices.SortFunc(rep.OffRadar, func(a, b cluster) int {
		return cmp.Or(cmp.Compare(b.Frames, a.Frames), cmp.Compare(a.PX, b.PX), cmp.Compare(a.PY, b.PY))
	})
	rep.OffRadar = rep.OffRadar[:min(top, len(rep.OffRadar))]
	return rep, nil
}

// checkMap fails when dir/match.json names a map other than the radar's.
func checkMap(dir, want string) error {
	var m struct {
		Match struct {
			Map string `json:"map"`
		} `json:"match"`
	}
	err := readJSON(filepath.Join(dir, "match.json"), &m)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.EqualFold(m.Match.Map, want) {
		return fmt.Errorf("%s is %s, but the radar is %s", dir, m.Match.Map, want)
	}
	return nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func rnd(v float64) int { return int(math.Round(v)) }

func (rep *report) print(w io.Writer) error {
	fmt.Fprintf(w, "%s: %.3f%% on the radar, %d of %d alive player frames (alpha > %d)\n",
		rep.Map, rep.Pct, rep.On, rep.Frames, rep.Alpha)
	for _, d := range rep.Demos {
		fmt.Fprintf(w, "\n%s: %.3f%% of %d\n", d.Name, d.Pct, d.Frames)
		for i, rd := range d.Rounds {
			if i%8 == 0 {
				fmt.Fprint(w, "\n ")
			}
			fmt.Fprintf(w, " %2d %7.3f", rd.N, rd.Pct)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "\nbusiest off-radar %d px cells (%d frames off in all):\n", rep.CellPx, rep.Frames-rep.On)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	fmt.Fprintln(tw, "frames\trounds\tx\ty\tpx\tpy\t")
	for _, c := range rep.OffRadar {
		fmt.Fprintf(tw, "%d\t%d\t%d\t%d\t%d\t%d\t\n", c.Frames, c.Rounds, c.X, c.Y, c.PX, c.PY)
	}
	return tw.Flush()
}
