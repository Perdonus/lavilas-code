#!/usr/bin/env bash
set -euo pipefail
VER_FILE="$(cd "$(dirname "$0")/.." && pwd)/VERSION"
[[ -f "$VER_FILE" ]] || printf '1.0.0\n' > "$VER_FILE"
cmd="${1:-show}"
ver="$(cat "$VER_FILE")"
if [[ "$cmd" == "show" ]]; then
  echo "$ver"
  exit 0
fi
if [[ "$cmd" == "set" ]]; then
  [[ -n "${2:-}" ]] || { echo "Usage: $0 set X.Y.Z" >&2; exit 1; }
  echo "$2" > "$VER_FILE"
  echo "$2"
  exit 0
fi
if [[ "$cmd" == "bump" ]]; then
  part="${2:-patch}"
  IFS='.' read -r major minor patch <<<"$ver"
  case "$part" in
    major) major=$((major+1)); minor=0; patch=0 ;;
    minor) minor=$((minor+1)); patch=0 ;;
    patch) patch=$((patch+1)) ;;
    *) echo "Usage: $0 bump [major|minor|patch]" >&2; exit 1 ;;
  esac
  next="${major}.${minor}.${patch}"
  echo "$next" > "$VER_FILE"
  echo "$next"
  exit 0
fi
echo "Usage: $0 [show|set X.Y.Z|bump major|minor|patch]" >&2
exit 1
