# syntax=docker/dockerfile:1

# The parser's Cloud Run image: cmd/server on distroless.
#
# Build from the repo root, so the stub fixtures in schema/samples are in the
# build context:
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

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/server /server
# Fixtures for stub mode, used only when PARSER_STUB_DIR=/fixtures.
COPY schema/samples/match.json /fixtures/match.json
COPY schema/samples/rounds/ /fixtures/rounds/
EXPOSE 8080
ENTRYPOINT ["/server"]
