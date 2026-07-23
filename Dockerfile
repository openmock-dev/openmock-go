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

# Runtime stage: a plain, ordinary Linux userland (not distroless) — a shell
# and package manager are useful for anyone who needs to exec in and poke
# around a running mock server. openmock itself is a static CGO_ENABLED=0
# binary, so it runs unmodified regardless of the base's libc.
FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --no-create-home --shell /usr/sbin/nologin openmock

COPY --from=build /out/openmock /usr/local/bin/openmock

USER openmock
WORKDIR /mocks

# http, graphql, websocket, grpc, admin — see docs/serving.md and README.md.
EXPOSE 3000 3001 3002 3003 4400

ENTRYPOINT ["openmock"]
