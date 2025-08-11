# syntax=docker/dockerfile:1.6

############################
# Builder
############################
FROM golang:1.22.9-alpine3.19 AS builder
WORKDIR /app

# Optional: keep your proxy; override at build-time if needed
ARG GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on \
    GOPROXY=${GOPROXY}

# 1) cache module downloads
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# 2) copy source AFTER deps to maximize cache hits
COPY . .

# 3) build with build + module caches
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -buildvcs=false -ldflags="-s -w" \
    -o /out/mqtt ./cmd/main.go

############################
# Runtime (Alpine, tiny)
############################
FROM alpine:3.20
WORKDIR /app

# TLS certs for HTTPS, optional but usually needed
RUN apk add --no-cache ca-certificates

# Copy app
COPY --from=builder /out/mqtt ./mqtt

# Copy configs you have in repo
COPY ./config.yaml ./config.yaml
COPY ./config-local.yaml ./config-local.yaml

# Helpful defaults; override at run-time if you like
ENV CONFIG_PATH=/app/config.yaml

# Drop privileges (optional but recommended)
# RUN adduser -D -H -u 10001 app && chown -R app:app /app
# USER app

ENTRYPOINT ["./mqtt"]
