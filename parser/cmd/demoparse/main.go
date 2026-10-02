// Command demoparse parses a CS2 demo file locally. It is the dev-loop entry
// point for the parser; the cloud worker will wrap the same packages.
package main

import (
	"fmt"
	"os"

	"github.com/ChiemekaKalu/demoview/parser/demo"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: demoparse <file.dem>")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	h, err := demo.ReadHeader(f)
	if err != nil {
		return err
	}
	fmt.Printf("CS2 demo: fileinfo@%d spawngroups@%d\n", h.FileInfoOffset, h.SpawnGroupsOffset)
	return nil
}
