package demo

import (
	"bytes"
	"errors"
	"testing"
)

func TestReadHeader(t *testing.T) {
	in := append(Magic[:], 0x10, 0x00, 0x00, 0x00, 0x20, 0x01, 0x00, 0x00)
	h, err := ReadHeader(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if h.FileInfoOffset != 0x10 || h.SpawnGroupsOffset != 0x120 {
		t.Fatalf("got %+v", h)
	}
}

func TestReadHeaderBadMagic(t *testing.T) {
	in := []byte("HL2DEMO\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	_, err := ReadHeader(bytes.NewReader(in))
	if !errors.Is(err, ErrBadMagic) || !errors.Is(err, ErrBadDemo) {
		t.Fatalf("want ErrBadMagic wrapping ErrBadDemo, got %v", err)
	}
}

func TestReadHeaderShort(t *testing.T) {
	if _, err := ReadHeader(bytes.NewReader(Magic[:4])); !errors.Is(err, ErrBadDemo) {
		t.Fatalf("want ErrBadDemo on truncated input, got %v", err)
	}
}
