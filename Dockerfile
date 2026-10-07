# syntax=docker/dockerfile:1
# Omini: one image with the Go binary (UI embedded), Python and uv for plugins.
# Multi-arch (amd64, arm64): the Go binary is cross-compiled, nothing is emulated.

FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG TARGETOS TARGETARCH VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
# App icons bundled for offline use (downloaded from Dashboard Icons).
RUN go run ./internal/appicons/gen -bundle
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/omini ./cmd/omini

FROM debian:trixie-slim
# Python for plugins (uv builds their environments with it), certificates
# for HTTPS integrations. nmap is not shipped: OMINI_NMAP=install adds it.
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates python3 tzdata \
 && rm -rf /var/lib/apt/lists/*
COPY --from=ghcr.io/astral-sh/uv:0.12.22 /uv /usr/local/bin/uv
COPY --from=build /out/omini /usr/local/bin/omini
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
ENV OMINI_DATA_DIR=/data \
    OMINI_ADDR=:8080 \
    UV_PYTHON_DOWNLOADS=never \
    UV_CACHE_DIR=/data/.cache/uv
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
