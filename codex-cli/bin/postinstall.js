#!/usr/bin/env node

import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import {
  PLATFORM_PACKAGE_BY_TARGET,
  detectPackageManager,
  ensurePlatformPackageMetadata,
  getCodexBinaryName,
  resolveTargetTriple,
  selectVendorInstallation,
  updateCommandForPackageManager,
} from "./platform-resolver.js";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const packageDir = path.join(__dirname, "..");
const require = createRequire(import.meta.url);

if (!packageDir.includes(`${path.sep}node_modules${path.sep}`)) {
  process.exit(0);
}

const targetTriple = resolveTargetTriple();
if (!targetTriple) {
  process.exit(0);
}

const platformPackage = PLATFORM_PACKAGE_BY_TARGET[targetTriple];
if (!platformPackage) {
  process.exit(0);
}

const rootPackageJson = JSON.parse(
  readFileSync(path.join(packageDir, "package.json"), "utf8"),
);
ensurePlatformPackageMetadata({
  packageDir: path.join(packageDir, "node_modules", ...platformPackage.split("/")),
  platformPackage,
  version: rootPackageJson.optionalDependencies?.[platformPackage] ?? rootPackageJson.version,
  license: rootPackageJson.license ?? "Apache-2.0",
  repository: rootPackageJson.repository ?? null,
  packageManager: rootPackageJson.packageManager ?? null,
});

const selectedInstallation = selectVendorInstallation({
  packageDir,
  platformPackage,
  targetTriple,
  binaryName: getCodexBinaryName(),
  requireResolve: (specifier) => require.resolve(specifier),
});

if (!selectedInstallation) {
  const packageManager = detectPackageManager({ installDir: __dirname });
  const updateCommand = updateCommandForPackageManager(packageManager);
  console.error(
    `[lavilas/codex] Failed to validate ${platformPackage}. Reinstall Codex: ${updateCommand}`,
  );
  process.exit(1);
}

const result = spawnSync(selectedInstallation.binaryPath, ["--version"], {
  stdio: "pipe",
  encoding: "utf8",
  timeout: 15000,
});

if (result.error) {
  console.error(
    `[lavilas/codex] Native binary validation failed: ${result.error.message}`,
  );
  process.exit(1);
}

if (result.signal) {
  console.error(
    `[lavilas/codex] Native binary exited with signal ${result.signal} during install validation.`,
  );
  process.exit(1);
}

if (result.status !== 0) {
  const details = (result.stderr || result.stdout || "").trim();
  if (details) {
    console.error(details);
  }
  process.exit(result.status ?? 1);
}
