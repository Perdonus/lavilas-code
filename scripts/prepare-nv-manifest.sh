#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
SOURCE_MANIFEST="$ROOT_DIR/nv.package.json"
DIST_DIR="$ROOT_DIR/dist"
OUTPUT_MANIFEST="${1:-$DIST_DIR/nv.package.publish.json}"
VERSION=${LAVILAS_VERSION:-}

if [[ ! -d "$DIST_DIR" ]]; then
  echo "dist directory is missing: $DIST_DIR" >&2
  exit 1
fi

if [[ -z "$VERSION" && -f "$DIST_DIR/release-metadata.json" ]]; then
  VERSION=$(
    python3 - "$DIST_DIR/release-metadata.json" <<'PY'
import json
import pathlib
import shutil
import sys

data = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
print(data.get("release", {}).get("version", ""))
PY
  )
fi

python3 - "$SOURCE_MANIFEST" "$OUTPUT_MANIFEST" "$VERSION" <<'PY'
import json
import pathlib
import shutil
import sys

source_manifest = pathlib.Path(sys.argv[1]).resolve()
output_manifest = pathlib.Path(sys.argv[2]).resolve()
version = sys.argv[3]

data = json.loads(source_manifest.read_text(encoding="utf-8"))
if version:
    data["version"] = version

missing_paths = []
output_manifest.parent.mkdir(parents=True, exist_ok=True)

readme_path = data.get("readme")
if readme_path:
    resolved_readme = (source_manifest.parent / readme_path).resolve()
    if not resolved_readme.exists():
        missing_paths.append(str(resolved_readme))
    staged_readme = output_manifest.parent / resolved_readme.name
    shutil.copy2(resolved_readme, staged_readme)
    data["readme"] = staged_readme.name

for variant in data.get("variants", []):
    artifact_path = variant.get("artifact")
    if not artifact_path:
        continue

    resolved_artifact = (source_manifest.parent / artifact_path).resolve()
    if not resolved_artifact.exists():
        missing_paths.append(str(resolved_artifact))
    staged_artifact = output_manifest.parent / resolved_artifact.name
    if resolved_artifact != staged_artifact:
        shutil.copy2(resolved_artifact, staged_artifact)
    variant["artifact"] = staged_artifact.name

if missing_paths:
    raise SystemExit(
        "manifest references missing files:\n" + "\n".join(sorted(set(missing_paths)))
    )

output_manifest.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
print(output_manifest)
PY
