#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: wait_for_npm_package.sh [--timeout SECONDS] [--interval SECONDS] <package@version>...

Wait until npm metadata is available and the published tarball responds with HTTP 200
for every specified package reference.
USAGE
}

timeout_seconds=900
interval_seconds=10
declare -a package_refs=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --timeout)
      timeout_seconds="$2"
      shift 2
      ;;
    --interval)
      interval_seconds="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      package_refs+=("$1")
      shift
      ;;
  esac
done

if [[ ${#package_refs[@]} -eq 0 ]]; then
  usage >&2
  exit 2
fi

for package_ref in "${package_refs[@]}"; do
  deadline=$((SECONDS + timeout_seconds))

  while true; do
    tarball="$(npm view "$package_ref" dist.tarball 2>/dev/null || true)"
    tarball="${tarball//$'\r'/}"
    tarball="${tarball//$'\n'/}"

    if [[ -n "$tarball" && "$tarball" != "undefined" && "$tarball" != "null" ]]; then
      if curl --silent --show-error --location --head --fail "$tarball" >/dev/null; then
        echo "Available: $package_ref -> $tarball"
        break
      fi
      echo "Tarball not reachable yet for $package_ref: $tarball"
    else
      echo "Metadata not ready yet for $package_ref"
    fi

    if (( SECONDS >= deadline )); then
      echo "Timed out waiting for $package_ref" >&2
      exit 1
    fi

    sleep "$interval_seconds"
  done
done
