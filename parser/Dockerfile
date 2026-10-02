# syntax=docker/dockerfile:1

# The parser's Cloud Run image: cmd/server on distroless, with tools/oracle
# beside it to write the v1 round files (the spec's fallback rule).
#
# Build from the repo root, so tools/oracle and the stub fixtures in
# schema/samples are in the build context:
#
#   docker build -f parser/Dockerfile -t demoview-parser .
#
# BuildKit trims the context with parser/Dockerfile.dockerignore.
#
# Unverified: no image has been built from this file yet. Treat it as a draft
# until CI builds it.

FROM golang:1.27 AS build
WORKDIR /src
COPY parser/go.mod parser/go.sum ./
RUN go mod download
COPY parser/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# The oracle is its own module (it wraps demoinfocs, which parser/ never
# imports), so it builds in its own stage.
FROM golang:1.27 AS oracle
WORKDIR /src
COPY tools/oracle/go.mod tools/oracle/go.sum ./
RUN go mod download
COPY tools/oracle/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/oracle .

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/server /server
COPY --from=oracle /out/oracle /usr/local/bin/oracle
# v1 ships the oracle's round files. Stub mode (PARSER_STUB_DIR) ignores this.
ENV PARSER_ROUNDS=oracle PARSER_ORACLE_BIN=/usr/local/bin/oracle
# Fixtures for stub mode, used only when PARSER_STUB_DIR=/fixtures.
COPY schema/samples/match.json /fixtures/match.json
COPY schema/samples/rounds/ /fixtures/rounds/
EXPOSE 8080
ENTRYPOINT ["/server"]
