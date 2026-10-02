# pb

Go types generated from Valve's CS2 protobuf definitions. The `.proto` files are vendored, unmodified, in `protos/`. The generated `*.pb.go` files are committed, so a normal `go build` needs no protobuf tooling.

## Pinned upstream

- Repo: [SteamDatabase/GameTracking-CS2](https://github.com/SteamDatabase/GameTracking-CS2), directory `Protobufs/`
- Commit: `6ac247908a83c309b37314fd097c47dc78103746` (2026-09-30T23:56:02Z), default branch `master`
- Game build at that commit: ClientVersion 2000922, PatchVersion 1.41.8.8 (`game/csgo/steam.inf`)

## Files

| File | Why it is here |
| ---- | -------------- |
| `demo.proto` | Demo frames: `EDemoCommands`, `CDemoFileHeader`, `CDemoFileInfo`, `CDemoPacket`, `CDemoFullPacket`, `CDemoSendTables`, `CDemoClassInfo`, `CDemoStringTables` |
| `netmessages.proto` | Packet messages: `SVC_Messages`, `CSVCMsg_ServerInfo`, `CSVCMsg_CreateStringTable`, `CSVCMsg_UpdateStringTable`, `CSVCMsg_PacketEntities`, `CSVCMsg_FlattenedSerializer` |
| `networkbasetypes.proto` | `NET_Messages`, `CNETMsg_Tick`, `CMsgPlayerInfo` (userinfo string table) |
| `gameevents.proto` | `EBaseGameEvents`, `CMsgSource1LegacyGameEventList`, `CMsgSource1LegacyGameEvent` |
| `source2_steam_stats.proto` | Imported by `netmessages.proto` |
| `network_connection.proto` | Imported by `networkbasetypes.proto` |
| `valveextensions.proto` | Imported by `networkbasetypes.proto` |

`google/protobuf/descriptor.proto` is also imported; it is a well-known type that buf supplies and that maps to `descriptorpb`.

Each vendored file is byte-identical to the upstream blob at the pinned commit. **No edits.** None of these files declares a `package`, `syntax` or `go_package`. They compile as proto2 in the empty package, and `buf.gen.yaml` maps each one to this Go package with protoc-gen-go `M` options. The seven files define no duplicate names.

## Regenerate

From this directory:

```sh
go generate
```

That runs `go run github.com/bufbuild/buf/cmd/buf@v1.73.0 generate`, which reads `buf.gen.yaml`. buf compiles the protos itself, so protoc is not needed. protoc-gen-go runs through `go run` at the `google.golang.org/protobuf` version in `parser/go.mod` (v1.36.12), so the generator always matches the runtime. Output is deterministic: regenerating unchanged inputs gives identical files.

To bump the pinned commit:

1. Copy the files above from `Protobufs/` at the new commit into `protos/`.
2. Run `go generate`.
3. Update the commit, date and build here.
4. Run `go test ./pb/...` from `parser/`. `TestEnumValues` fails if Valve renumbered any wire id the parser depends on.

To add a file, also add its transitive imports, and add an `M<file>.proto=github.com/ChiemekaKalu/demoview/parser/pb;pb` line for each one to `buf.gen.yaml`.

## Notes for users of this package

- Messages are registered under bare names, such as `CDemoFileHeader`, with no proto package prefix.
- The files are proto2, so scalar fields are pointers. Use the `GetX()` accessors.
- `CDemoFileHeader.demo_file_stamp` is `required`. A plain `proto.Unmarshal` returns an error if it is missing, unless you use `proto.UnmarshalOptions{AllowPartial: true}`.
- An enum field holding a value that the vendored file doesn't define still decodes. The Go runtime keeps the raw number in the field, so check it against the generated constants rather than trusting it. Wire ids read straight from the bitstream (command types, message types) are plain integers, so map them yourself.
