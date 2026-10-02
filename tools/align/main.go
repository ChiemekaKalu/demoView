// Command align checks how well a map's radar lines up with player positions:
// the share of alive player frames that land on a non-transparent radar pixel.
//
// Usage:
//
//	align --radar <png> --info <radar_info.txt> [--alpha 0] [--top 10] [--min 97.5] [--json] <round file | parse dir>...
//	align fetch [--dir .cache] <map>...
//
// A parse dir holds match.json and rounds/NN.json. When a round file has a
// ../match.json, its map must be the radar's. The check fails when any demo
// is below --min, a bar measured on the local Mirage, Ancient, Inferno and
// Overpass demos. fetch downloads radars and
// radar_info files from a pinned commit of MurkyYT/cs2-map-icons into a
// gitignored cache. They are unlicensed, so the check runs locally, not in CI.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ChiemekaKalu/demoview/tools/align/radar"
)

const usage = `usage: align --radar <png> --info <radar_info.txt> [--alpha 0] [--top 10] [--min 97.5] [--json] <round file | parse dir>...
       align fetch [--dir .cache] <map>...`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "align:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fetching := len(args) > 0 && args[0] == "fetch"
	if fetching {
		args = args[1:]
	}
	fs := flag.NewFlagSet("align", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", ".cache", "fetch: cache directory")
	radarPath := fs.String("radar", "", "radar PNG")
	infoPath := fs.String("info", "", "radar_info file")
	alpha := fs.Uint("alpha", 0, "a pixel is on the radar when its alpha is above this")
	top := fs.Int("top", 10, "off-radar cells to list")
	minPct := fs.Float64("min", 97.5, "fail when a demo's share is below this percentage")
	asJSON := fs.Bool("json", false, "print JSON")
	// Accept positional arguments between flags.
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return fmt.Errorf("%v\n%s", err, usage)
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	if len(pos) == 0 {
		return errors.New(usage)
	}
	if fetching {
		return fetch(*dir, pos, stdout)
	}
	if *radarPath == "" || *infoPath == "" || *alpha > 254 || *top < 0 {
		return errors.New(usage)
	}

	r, err := radar.Load(*radarPath, *infoPath)
	if err != nil {
		return err
	}
	files, err := roundFiles(pos)
	if err != nil {
		return err
	}
	rep, err := measure(r, files, uint8(*alpha), *top)
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(rep)
	} else {
		err = rep.print(stdout)
	}
	if err != nil {
		return err
	}
	var low []string
	for _, d := range rep.Demos {
		if d.Pct < *minPct {
			low = append(low, fmt.Sprintf("%s %.3f%%", d.Name, d.Pct))
		}
	}
	if len(low) > 0 {
		return fmt.Errorf("below the %g%% bar: %s", *minPct, strings.Join(low, ", "))
	}
	return nil
}
