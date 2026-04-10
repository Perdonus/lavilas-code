#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: wait_for_npm_package.sh [--timeout SECONDS] [--interval SECONDS] <package@version>...

Wait until npm metadata is available and npm can resolve the exact package
reference via `npm pack`.
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
      pack_dir="$(mktemp -d)"
      if (cd "$pack_dir" && npm pack "$package_ref" --silent >/dev/null 2>&1); then
        rm -rf "$pack_dir"
        echo "Available: $package_ref -> $tarball"
        break
      fi
      rm -rf "$pack_dir"
      echo "Metadata ready but npm pack still fails for $package_ref"
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
