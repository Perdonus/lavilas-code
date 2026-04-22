#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
DIST_DIR="$ROOT_DIR/dist"
BUILD_DIR=$(mktemp -d "${TMPDIR:-/tmp}/lvls-release.XXXXXX")
PRODUCT_SLUG=${GO_LAVILAS_PRODUCT_SLUG:-lvls}
PRODUCT_TITLE=${GO_LAVILAS_PRODUCT_TITLE:-Go Lavilas Alpha}
PRODUCT_BINARY=${GO_LAVILAS_BINARY_NAME:-lvls}
RELEASE_CHANNEL=${GO_LAVILAS_CHANNEL:-alpha}
VERSION=${LAVILAS_VERSION:-0.1.0-alpha.35}
COMMIT=${LAVILAS_COMMIT:-dev}
GIT_REF=${LAVILAS_GIT_REF:-local}
BUILD_DATE=${LAVILAS_BUILD_DATE:-$(date -u +"%Y-%m-%dT%H:%M:%SZ")}
REPOSITORY_URL=${LAVILAS_REPOSITORY_URL:-}
WORKFLOW_RUN_URL=${LAVILAS_WORKFLOW_RUN_URL:-}
WORKFLOW_RUN_ID=${GITHUB_RUN_ID:-}
WORKFLOW_RUN_NUMBER=${GITHUB_RUN_NUMBER:-}
SOURCE_REPOSITORY=${GITHUB_REPOSITORY:-}
GO_TOOLCHAIN_VERSION=${LAVILAS_GO_VERSION:-$(go env GOVERSION 2>/dev/null || true)}
MANIFEST_TEMPLATE=${LAVILAS_MANIFEST_TEMPLATE:-nv.package.json}
LDFLAGS="-s -w -X github.com/Perdonus/lavilas-code/internal/version.Version=$VERSION -X github.com/Perdonus/lavilas-code/internal/version.Commit=$COMMIT"

trap 'rm -rf "$BUILD_DIR"' EXIT

checksum_file() {
  local path="$1"

  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
    return
  fi

  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$path" | awk '{print $1}'
    return
  fi

  echo "sha256 checksum tool is required" >&2
  exit 1
}

build_linux_tarball() {
  local arch="$1"
  local stage_dir="$BUILD_DIR/linux-$arch"
  local archive_path="$DIST_DIR/${PRODUCT_SLUG}-linux-$arch.tar.gz"

  mkdir -p "$stage_dir"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$stage_dir/$PRODUCT_BINARY" ./cmd/lvls
  tar -C "$stage_dir" -czf "$archive_path" "$PRODUCT_BINARY"
}

build_windows_binary() {
  local arch="$1"
  local output_path="$DIST_DIR/${PRODUCT_SLUG}-windows-$arch.exe"

  CGO_ENABLED=0 GOOS=windows GOARCH="$arch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$output_path" ./cmd/lvls
}

write_release_metadata() {
  local linux_amd64_hash="$1"
  local linux_arm64_hash="$2"
  local windows_amd64_hash="$3"

  PRODUCT_SLUG="$PRODUCT_SLUG" \
  PRODUCT_TITLE="$PRODUCT_TITLE" \
  PRODUCT_BINARY="$PRODUCT_BINARY" \
  RELEASE_CHANNEL="$RELEASE_CHANNEL" \
  VERSION="$VERSION" \
  COMMIT="$COMMIT" \
  GIT_REF="$GIT_REF" \
  BUILD_DATE="$BUILD_DATE" \
  GO_TOOLCHAIN_VERSION="$GO_TOOLCHAIN_VERSION" \
  SOURCE_REPOSITORY="$SOURCE_REPOSITORY" \
  REPOSITORY_URL="$REPOSITORY_URL" \
  WORKFLOW_RUN_ID="$WORKFLOW_RUN_ID" \
  WORKFLOW_RUN_NUMBER="$WORKFLOW_RUN_NUMBER" \
  WORKFLOW_RUN_URL="$WORKFLOW_RUN_URL" \
  MANIFEST_TEMPLATE="$MANIFEST_TEMPLATE" \
  LINUX_AMD64_HASH="$linux_amd64_hash" \
  LINUX_ARM64_HASH="$linux_arm64_hash" \
  WINDOWS_AMD64_HASH="$windows_amd64_hash" \
    python3 - "$DIST_DIR/release-metadata.json" <<'PY'
import json
import os
import pathlib
import sys

output_path = pathlib.Path(sys.argv[1])

data = {
    "product": {
        "id": os.environ["PRODUCT_SLUG"],
        "title": os.environ["PRODUCT_TITLE"],
        "binary": os.environ["PRODUCT_BINARY"],
        "channel": os.environ["RELEASE_CHANNEL"],
    },
    "release": {
        "version": os.environ["VERSION"],
        "commit": os.environ["COMMIT"],
        "git_ref": os.environ["GIT_REF"],
        "built_at": os.environ["BUILD_DATE"],
        "go_version": os.environ["GO_TOOLCHAIN_VERSION"],
        "repository": os.environ["SOURCE_REPOSITORY"],
        "repository_url": os.environ["REPOSITORY_URL"],
        "workflow_run_id": os.environ["WORKFLOW_RUN_ID"],
        "workflow_run_number": os.environ["WORKFLOW_RUN_NUMBER"],
        "workflow_run_url": os.environ["WORKFLOW_RUN_URL"],
        "manifest_template": os.environ["MANIFEST_TEMPLATE"],
    },
    "artifacts": [
        {
            "id": f'{os.environ["PRODUCT_SLUG"]}-linux-amd64',
            "os": "linux",
            "arch": "amd64",
            "path": f'dist/{os.environ["PRODUCT_SLUG"]}-linux-amd64.tar.gz',
            "file_name": f'{os.environ["PRODUCT_SLUG"]}-linux-amd64.tar.gz',
            "packaging": "tar.gz",
            "install_strategy": "linux-portable-tar",
            "sha256": os.environ["LINUX_AMD64_HASH"],
        },
        {
            "id": f'{os.environ["PRODUCT_SLUG"]}-linux-arm64',
            "os": "linux",
            "arch": "arm64",
            "path": f'dist/{os.environ["PRODUCT_SLUG"]}-linux-arm64.tar.gz',
            "file_name": f'{os.environ["PRODUCT_SLUG"]}-linux-arm64.tar.gz',
            "packaging": "tar.gz",
            "install_strategy": "linux-portable-tar",
            "sha256": os.environ["LINUX_ARM64_HASH"],
        },
        {
            "id": f'{os.environ["PRODUCT_SLUG"]}-windows-amd64',
            "os": "windows",
            "arch": "amd64",
            "path": f'dist/{os.environ["PRODUCT_SLUG"]}-windows-amd64.exe',
            "file_name": f'{os.environ["PRODUCT_SLUG"]}-windows-amd64.exe',
            "packaging": "exe",
            "install_strategy": "windows-self-binary",
            "sha256": os.environ["WINDOWS_AMD64_HASH"],
        },
    ],
}

output_path.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
PY
}

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

build_linux_tarball amd64
build_linux_tarball arm64
build_windows_binary amd64

LINUX_AMD64_FILE="$DIST_DIR/${PRODUCT_SLUG}-linux-amd64.tar.gz"
LINUX_ARM64_FILE="$DIST_DIR/${PRODUCT_SLUG}-linux-arm64.tar.gz"
WINDOWS_AMD64_FILE="$DIST_DIR/${PRODUCT_SLUG}-windows-amd64.exe"

LINUX_AMD64_HASH=$(checksum_file "$LINUX_AMD64_FILE")
LINUX_ARM64_HASH=$(checksum_file "$LINUX_ARM64_FILE")
WINDOWS_AMD64_HASH=$(checksum_file "$WINDOWS_AMD64_FILE")

cat > "$DIST_DIR/SHA256SUMS" <<SUMS
$LINUX_AMD64_HASH  $(basename "$LINUX_AMD64_FILE")
$LINUX_ARM64_HASH  $(basename "$LINUX_ARM64_FILE")
$WINDOWS_AMD64_HASH  $(basename "$WINDOWS_AMD64_FILE")
SUMS

write_release_metadata "$LINUX_AMD64_HASH" "$LINUX_ARM64_HASH" "$WINDOWS_AMD64_HASH"

echo "$DIST_DIR"
