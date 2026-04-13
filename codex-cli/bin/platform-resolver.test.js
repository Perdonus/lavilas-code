import test from "node:test";
import assert from "node:assert/strict";
import os from "node:os";
import path from "node:path";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import {
  ensurePlatformPackageMetadata,
  selectVendorInstallation,
} from "./platform-resolver.js";

function makeVendorTree(rootDir, size = 16) {
  const vendorRoot = path.join(rootDir, "vendor");
  const targetTriple = "x86_64-unknown-linux-musl";
  const binaryPath = path.join(vendorRoot, targetTriple, "codex", "codex");
  mkdirSync(path.dirname(binaryPath), { recursive: true });
  writeFileSync(binaryPath, Buffer.alloc(size, 1));
  writeFileSync(
    path.join(vendorRoot, "manifest.json"),
    JSON.stringify(
      {
        version: 1,
        files: {
          [`${targetTriple}/codex/codex`]: {
            size,
            sha256: "ignored-in-runtime-test",
          },
        },
      },
      null,
      2,
    ),
  );
  return { vendorRoot, binaryPath };
}

test("selectVendorInstallation uses nested vendor even without package.json", () => {
  const tempRoot = path.join(os.tmpdir(), `codex-platform-test-${Date.now()}-nested`);
  rmSync(tempRoot, { force: true, recursive: true });
  const packageDir = path.join(tempRoot, "node_modules", "@lavilas", "codex");
  const nestedPlatformDir = path.join(
    packageDir,
    "node_modules",
    "@lavilas",
    "codex-linux-x64",
  );
  makeVendorTree(nestedPlatformDir, 24);

  const selected = selectVendorInstallation({
    packageDir,
    platformPackage: "@lavilas/codex-linux-x64",
    targetTriple: "x86_64-unknown-linux-musl",
    binaryName: "codex",
    requireResolve: () => {
      throw new Error("not installed");
    },
  });

  assert.ok(selected);
  assert.equal(selected.source, "nested-platform-package");
  assert.equal(
    selected.binaryPath,
    path.join(nestedPlatformDir, "vendor", "x86_64-unknown-linux-musl", "codex", "codex"),
  );

  rmSync(tempRoot, { force: true, recursive: true });
});

test("selectVendorInstallation falls back to staged sibling when current binary is truncated", () => {
  const tempRoot = path.join(os.tmpdir(), `codex-platform-test-${Date.now()}-fallback`);
  rmSync(tempRoot, { force: true, recursive: true });

  const scopeDir = path.join(tempRoot, "node_modules", "@lavilas");
  const packageDir = path.join(scopeDir, "codex");
  const currentPlatformDir = path.join(
    packageDir,
    "node_modules",
    "@lavilas",
    "codex-linux-x64",
  );
  const stagedPackageDir = path.join(scopeDir, ".codex-fallback");
  const stagedPlatformDir = path.join(
    stagedPackageDir,
    "node_modules",
    "@lavilas",
    "codex-linux-x64",
  );

  makeVendorTree(currentPlatformDir, 64);
  writeFileSync(
    path.join(currentPlatformDir, "vendor", "x86_64-unknown-linux-musl", "codex", "codex"),
    Buffer.alloc(8, 1),
  );
  makeVendorTree(stagedPlatformDir, 64);

  const selected = selectVendorInstallation({
    packageDir,
    platformPackage: "@lavilas/codex-linux-x64",
    targetTriple: "x86_64-unknown-linux-musl",
    binaryName: "codex",
    requireResolve: () => {
      throw new Error("not installed");
    },
  });

  assert.ok(selected);
  assert.match(selected.source, /^staged-platform-package:/);
  assert.equal(
    selected.binaryPath,
    path.join(stagedPlatformDir, "vendor", "x86_64-unknown-linux-musl", "codex", "codex"),
  );

  rmSync(tempRoot, { force: true, recursive: true });
});

test("ensurePlatformPackageMetadata recreates missing package.json from vendor tree", () => {
  const tempRoot = path.join(os.tmpdir(), `codex-platform-test-${Date.now()}-metadata`);
  rmSync(tempRoot, { force: true, recursive: true });
  const platformDir = path.join(tempRoot, "node_modules", "@lavilas", "codex-linux-x64");
  makeVendorTree(platformDir, 12);

  const packageJsonPath = ensurePlatformPackageMetadata({
    packageDir: platformDir,
    platformPackage: "@lavilas/codex-linux-x64",
    version: "1.3.56",
    repository: {
      type: "git",
      url: "git+https://github.com/Perdonus/lavilas-code.git",
      directory: "codex-cli",
    },
    packageManager: "pnpm@test",
  });

  assert.equal(packageJsonPath, path.join(platformDir, "package.json"));
  const written = JSON.parse(readFileSync(packageJsonPath, "utf8"));
  assert.equal(written.name, "@lavilas/codex-linux-x64");
  assert.equal(written.version, "1.3.56");

  rmSync(tempRoot, { force: true, recursive: true });
});
