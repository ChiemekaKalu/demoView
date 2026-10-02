package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"

	"github.com/ChiemekaKalu/demoview/parser/demo"
	"github.com/ChiemekaKalu/demoview/parser/match"
)

// parserVersion is recorded in match.json and in the parse log. It matches
// cmd/demo's; both move to the shared parse package when it exists.
const parserVersion = "0.1.0"

// streamParse is the server's ParseFunc until the parse step moves out of
// cmd/demo into a library package. It does what cmd/demo's parse does today:
// stream every frame to EOF and hash the demo on the way through.
func streamParse(ctx context.Context, r io.Reader) (match.File, error) {
	sum := sha256.New()
	dr, err := demo.NewReader(io.TeeReader(r, sum))
	if err != nil {
		return match.File{}, err
	}
	for n := 0; ; n++ {
		if n%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return match.File{}, err
			}
		}
		_, err := dr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return match.File{}, err
		}
	}
	return match.File{
		SchemaVersion: match.SchemaVersion,
		ParserVersion: parserVersion,
		Match:         match.Info{DemoSHA256: hex.EncodeToString(sum.Sum(nil))},
	}, nil
}
