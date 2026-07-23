# syntax=docker/dockerfile:1

# Build stage: Chainguard's Wolfi-based Go image — a minimal, actively
# patched, low-CVE build environment (https://github.com/chainguard-images).
# Only this stage ever sees the module cache and source tree.
FROM cgr.dev/chainguard/go:latest AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY engine ./engine
COPY server ./server

ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/openmock \
    ./cmd/openmock

# Runtime stage: Chainguard's distroless "static" image — no shell, no
# package manager, nothing beyond ca-certificates and a nonroot user entry.
# openmock is a single static (CGO_ENABLED=0) binary and needs nothing else
# to run, so the smallest and lowest-CVE base wins over shell-debuggability.
FROM cgr.dev/chainguard/static:latest AS runtime

COPY --from=build /out/openmock /usr/local/bin/openmock

WORKDIR /mocks

# Only the admin API has a port fixed by this image: it always binds (spec
# docs/serving.md §3), defaulting to 4400 unless --admin-port overrides it.
# The protocol servers' ports are resolved per document (flags/env, then the
# document's port hint, then a stable default only for a document's sole
# server of a type, else an OS-ephemeral port) — EXPOSE can't describe a
# moving target, so publish those explicitly at `docker run` time; see the
# Docker section of README.md.
EXPOSE 4400

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/openmock"]
