# syntax=docker/dockerfile:1
# Multi-stage like apps/web/Dockerfile, but Go's build story is different: no volume-mounted
# dependency cache to reconcile, no dev-vs-test split — `serve`/`work`/`migrate` are the same
# binary, invoked with different args (compose.yaml's api/worker services), not different
# images.
FROM golang:1.25-bookworm AS build
ARG GIT_SHA=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-X main.gitSHA=${GIT_SHA}" -o /out/holdmytrack ./cmd/holdmytrack

# distroless/static: the one binary is pure Go (CGO_ENABLED=0 above) and runs nothing else —
# the Takeout reader that used to be a Rust subprocess is internal/takeout now — so it needs
# no libc. No shell and no package manager.
FROM gcr.io/distroless/static-debian12 AS dev
COPY --from=build /out/holdmytrack /usr/local/bin/holdmytrack
ENTRYPOINT ["/usr/local/bin/holdmytrack"]
