// Package radar places a CS2 radar image in game space, from the image and
// its radar_info file.
package radar

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// Info is a radar_info file's placement of the radar image.
type Info struct {
	Name  string  // the root block's key, the map name
	PosX  float64 // game x of the image's left edge
	PosY  float64 // game y of the image's top edge
	Scale float64 // game units per pixel
}

// Pixel maps a game position onto the image. Game y points up, image y down.
func (i Info) Pixel(x, y float64) (px, py float64) {
	return (x - i.PosX) / i.Scale, (i.PosY - y) / i.Scale
}

// Radar is a placement plus the image's alpha channel.
type Radar struct {
	Info
	Alpha *image.Alpha
}

// Load reads a radar PNG and its radar_info file.
func Load(pngPath, infoPath string) (*Radar, error) {
	src, err := os.ReadFile(infoPath)
	if err != nil {
		return nil, err
	}
	info, err := ParseInfo(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", infoPath, err)
	}
	f, err := os.Open(pngPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", pngPath, err)
	}
	return &Radar{Info: info, Alpha: AlphaOf(img)}, nil
}

// AlphaOf copies an image's alpha channel.
func AlphaOf(img image.Image) *image.Alpha {
	a := image.NewAlpha(img.Bounds())
	draw.Draw(a, a.Rect, img, a.Rect.Min, draw.Src)
	return a
}

// Opaque reports whether pixel (px, py) has alpha above the threshold.
// AlphaAt is zero off the image, so out-of-bounds pixels are never opaque.
func (r *Radar) Opaque(px, py float64, above uint8) bool {
	return r.Alpha.AlphaAt(int(math.Floor(px)), int(math.Floor(py))).A > above
}

// ParseInfo reads a radar_info file, Valve's KeyValues text format: a root
// block named after the map that holds "pos_x", "pos_y" and "scale".
func ParseInfo(src string) (Info, error) {
	toks, err := tokenize(src)
	if err != nil {
		return Info{}, err
	}
	var name, key string
	vals := map[string]string{}
	depth := 0
	for _, t := range toks {
		switch {
		case t.brace && t.text == "{":
			if depth == 0 {
				name = key
			}
			depth++
			key = ""
		case t.brace:
			if depth--; depth < 0 {
				return Info{}, errors.New("unbalanced braces")
			}
		case key == "":
			key = strings.ToLower(t.text)
		default:
			if depth == 1 {
				vals[key] = t.text
			}
			key = ""
		}
	}
	if depth != 0 || name == "" {
		return Info{}, errors.New("no closed root block")
	}
	info := Info{Name: name}
	for _, f := range []struct {
		key string
		dst *float64
	}{{"pos_x", &info.PosX}, {"pos_y", &info.PosY}, {"scale", &info.Scale}} {
		v, err := strconv.ParseFloat(vals[f.key], 64)
		if err != nil {
			return Info{}, fmt.Errorf("missing or bad %s", f.key)
		}
		*f.dst = v
	}
	if info.Scale <= 0 {
		return Info{}, errors.New("scale must be positive")
	}
	return info, nil
}

type token struct {
	text  string
	brace bool // an unquoted { or }
}

func tokenize(s string) ([]token, error) {
	var toks []token
	for {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		switch {
		case s == "":
			return toks, nil
		case strings.HasPrefix(s, "//"):
			_, s, _ = strings.Cut(s, "\n")
		case s[0] == '{' || s[0] == '}':
			toks = append(toks, token{s[:1], true})
			s = s[1:]
		case s[0] == '"':
			text, rest, ok := strings.Cut(s[1:], `"`)
			if !ok {
				return nil, errors.New("unterminated string")
			}
			toks = append(toks, token{text: text})
			s = rest
		default:
			n := strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`"{}`, r) })
			if n < 0 {
				n = len(s)
			}
			toks = append(toks, token{text: s[:n]})
			s = s[n:]
		}
	}
}
