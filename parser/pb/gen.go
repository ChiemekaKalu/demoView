// Package pb holds Go types generated from Valve's CS2 protobuf definitions,
// vendored in ./protos from SteamDatabase/GameTracking-CS2. See README.md for
// the pinned upstream commit and the file list.
//
// The generated *.pb.go files are committed, so normal builds need no protobuf
// tooling. To regenerate after changing ./protos or buf.gen.yaml, run from
// this directory:
//
//	go generate
//
// That builds buf (pinned below) and protoc-gen-go (pinned by parser/go.mod)
// with the Go toolchain; protoc is not needed.
package pb

//go:generate go run github.com/bufbuild/buf/cmd/buf@v1.73.0 generate
