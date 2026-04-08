#!/usr/bin/env python3
"""Fetch the vendored ripgrep binary for a single npm package target."""

from __future__ import annotations

import argparse
import importlib.util
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
INSTALL_NATIVE_DEPS_PATH = REPO_ROOT / "codex-cli" / "scripts" / "install_native_deps.py"
RG_MANIFEST_PATH = REPO_ROOT / "codex-cli" / "bin" / "rg"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fetch the ripgrep payload for a single target triple.",
    )
    parser.add_argument(
        "--vendor-dir",
        type=Path,
        required=True,
        help="Vendor root where target payloads should be staged.",
    )
    parser.add_argument(
        "--target",
        required=True,
        help="Target triple to fetch ripgrep for.",
    )
    return parser.parse_args()


def load_install_native_deps_module():
    spec = importlib.util.spec_from_file_location(
        "install_native_deps",
        INSTALL_NATIVE_DEPS_PATH,
    )
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Unable to load install_native_deps from {INSTALL_NATIVE_DEPS_PATH}")

    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main() -> int:
    args = parse_args()
    module = load_install_native_deps_module()
    module.fetch_rg(
        args.vendor_dir.resolve(),
        targets=[args.target],
        manifest_path=RG_MANIFEST_PATH,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
