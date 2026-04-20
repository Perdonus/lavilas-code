#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
DIST_DIR="$ROOT_DIR/dist"
VERSION=${LAVILAS_VERSION:-0.1.0-alpha.1}
COMMIT=${LAVILAS_COMMIT:-dev}
LDFLAGS="-s -w -X github.com/Perdonus/lavilas-code/internal/version.Version=$VERSION -X github.com/Perdonus/lavilas-code/internal/version.Commit=$COMMIT"

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR/linux-amd64"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$DIST_DIR/linux-amd64/lavilas" ./cmd/lavilas
tar -C "$DIST_DIR/linux-amd64" -czf "$DIST_DIR/lavilas-linux-amd64.tar.gz" lavilas

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$DIST_DIR/lavilas-windows-amd64.exe" ./cmd/lavilas

echo "$DIST_DIR"
