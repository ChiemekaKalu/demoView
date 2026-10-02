package radar

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestPixel(t *testing.T) {
	i := Info{PosX: -3230, PosY: 1713, Scale: 5}
	for _, c := range [][4]float64{
		{-3230, 1713, 0, 0},
		{-3230 + 5*1024, 1713 - 5*1024, 1024, 1024},
		{-3180, 1688, 10, 5},
		{-3240, 1723, -2, -2},
	} {
		if px, py := i.Pixel(c[0], c[1]); px != c[2] || py != c[3] {
			t.Errorf("Pixel(%v, %v) = (%v, %v), want (%v, %v)", c[0], c[1], px, py, c[2], c[3])
		}
	}
}

const mirage = `// HLTV overview description file
"de_mirage"
{
	"material"	"overviews/de_mirage"	// texture file
	"POS_X"		"-3230"	// X coordinate,
	pos_y		1713
	"scale"		"5.00" 	// scale
	"verticalsections"
	{
		"lower" { "scale" "9" "AltitudeMin" "-10000" }
	}
	"note"	"{ not a block }"
}
`

func TestParseInfo(t *testing.T) {
	got, err := ParseInfo(mirage)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Info{Name: "de_mirage", PosX: -3230, PosY: 1713, Scale: 5}); got != want {
		t.Errorf("ParseInfo = %+v, want %+v", got, want)
	}
	for name, src := range map[string]string{
		"no scale":     `"m" { "pos_x" "1" "pos_y" "2" }`,
		"bad number":   `"m" { "pos_x" "1" "pos_y" "two" "scale" "5" }`,
		"zero scale":   `"m" { "pos_x" "1" "pos_y" "2" "scale" "0" }`,
		"unclosed":     `"m" { "pos_x" "1" "pos_y" "2" "scale" "5"`,
		"extra brace":  `"m" { "pos_x" "1" "pos_y" "2" "scale" "5" } }`,
		"open string":  `"m" { "pos_x" "1`,
		"nested only":  `"m" { "sub" { "pos_x" "1" "pos_y" "2" "scale" "5" } }`,
		"empty":        ``,
		"comment only": `// "m" { "pos_x" "1" "pos_y" "2" "scale" "5" }`,
	} {
		if _, err := ParseInfo(src); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// TestOpaque builds a 4x1 radar with alphas 0, 1, 128 and 255 and loads it
// through a PNG file.
func TestOpaque(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x, a := range []uint8{0, 1, 128, 255} {
		img.SetNRGBA(x, 0, color.NRGBA{200, 100, 50, a})
	}
	dir := t.TempDir()
	pngPath, infoPath := filepath.Join(dir, "r.png"), filepath.Join(dir, "r.txt")
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.WriteFile(infoPath, []byte(`"de_test" { "pos_x" "0" "pos_y" "0" "scale" "1" }`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Load(pngPath, infoPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		px, py float64
		above  uint8
		want   bool
	}{
		{0.5, 0.5, 0, false},
		{1, 0, 0, true},
		{1.99, 0.99, 0, true},
		{1, 0, 1, false},
		{2, 0, 127, true},
		{2, 0, 128, false},
		{3.5, 0, 254, true},
		{-0.01, 0, 0, false},
		{4, 0, 0, false},
		{3, 1, 0, false},
		{3, -0.5, 0, false},
	} {
		if got := r.Opaque(c.px, c.py, c.above); got != c.want {
			t.Errorf("Opaque(%v, %v, %d) = %v, want %v", c.px, c.py, c.above, got, c.want)
		}
	}
}
