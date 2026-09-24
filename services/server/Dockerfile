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

# The Takeout import path (internal/httpapi/takeout_upload.go) shells out to `pathify`
# (github.com/np25071984/pathify) rather than reimplementing its Google Health/Fitbit
# export-joining logic in Go — see that file's own doc comment for why. Pinned to a specific
# version deliberately: bumping it is a decision to make on purpose, not something that should
# drift because crates.io published something newer between two otherwise-unrelated builds.
FROM rust:1.88-bookworm AS pathify-build
RUN cargo install pathify-cli --version 1.4.0 --locked

# distroless/cc, not distroless/static: `holdmytrack` itself is a static Go binary either image
# would run, but `pathify` is a normal dynamically-linked Rust binary and needs libc/libgcc,
# which only the cc variant provides. Still no shell and no package manager either way.
FROM gcr.io/distroless/cc-debian12 AS dev
COPY --from=build /out/holdmytrack /usr/local/bin/holdmytrack
COPY --from=pathify-build /usr/local/cargo/bin/pathify /usr/local/bin/pathify
ENTRYPOINT ["/usr/local/bin/holdmytrack"]
