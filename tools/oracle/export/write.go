package export

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteDir writes res to dir as match.json and rounds/NN.json, creating
// directories as needed. match.json is indented; round files are compact,
// since they are large and only machines read them.
func WriteDir(dir string, res *Result) error {
	if err := os.MkdirAll(filepath.Join(dir, "rounds"), 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "match.json"), res.Match, true); err != nil {
		return err
	}
	for _, rf := range res.Rounds {
		name := filepath.Join(dir, "rounds", RoundFileName(rf.Round))
		if err := writeJSON(name, rf, false); err != nil {
			return err
		}
	}
	return nil
}

// RoundFileName is a round's file name: the number zero-padded to at least
// two digits.
func RoundFileName(n int) string {
	return fmt.Sprintf("%02d.json", n)
}

func writeJSON(path string, v any, indent bool) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent {
		enc.SetIndent("", "  ")
	}
	if err := enc.Encode(v); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
