import {
  chmodSync,
  copyFileSync,
  existsSync,
  lstatSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  realpathSync,
  renameSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { createHash } from "node:crypto";
import os from "node:os";
import path from "node:path";

export const PLATFORM_PACKAGE_BY_TARGET = {
  "x86_64-unknown-linux-musl": "@lavilas/codex-linux-x64",
  "aarch64-unknown-linux-musl": "@lavilas/codex-linux-arm64",
  "x86_64-apple-darwin": "@lavilas/codex-darwin-x64",
  "aarch64-apple-darwin": "@lavilas/codex-darwin-arm64",
  "x86_64-pc-windows-msvc": "@lavilas/codex-win32-x64",
  "aarch64-pc-windows-msvc": "@lavilas/codex-win32-arm64",
};

export function resolveTargetTriple(
  platformName = process.platform,
  archName = process.arch,
) {
  switch (platformName) {
    case "linux":
    case "android":
      switch (archName) {
        case "x64":
          return "x86_64-unknown-linux-musl";
        case "arm64":
          return "aarch64-unknown-linux-musl";
        default:
          return null;
      }
    case "darwin":
      switch (archName) {
        case "x64":
          return "x86_64-apple-darwin";
        case "arm64":
          return "aarch64-apple-darwin";
        default:
          return null;
      }
    case "win32":
      switch (archName) {
        case "x64":
          return "x86_64-pc-windows-msvc";
        case "arm64":
          return "aarch64-pc-windows-msvc";
        default:
          return null;
      }
    default:
      return null;
  }
}

export function getCodexBinaryName(platformName = process.platform) {
  return platformName === "win32" ? "codex.exe" : "codex";
}

export function detectPackageManager(options = {}) {
  const userAgent = options.userAgent ?? process.env.npm_config_user_agent ?? "";
  if (/\bbun\//.test(userAgent)) {
    return "bun";
  }

  const execPath = options.execPath ?? process.env.npm_execpath ?? "";
  if (execPath.includes("bun")) {
    return "bun";
  }

  const installDir = options.installDir ?? "";
  if (
    installDir.includes(".bun/install/global") ||
    installDir.includes(".bun\\install\\global")
  ) {
    return "bun";
  }

  return userAgent ? "npm" : null;
}

export function updateCommandForPackageManager(packageManager) {
  return packageManager === "bun"
    ? "bun install -g @lavilas/codex@latest"
    : "npm install -g @lavilas/codex@latest";
}

function packageNameSegments(packageName) {
  return packageName.split("/");
}

function packageInstallDir(packageDir, platformPackage) {
  return path.join(packageDir, "node_modules", ...packageNameSegments(platformPackage));
}

function packageCacheBaseDir(runtimeCacheRoot, platformPackage) {
  if (!runtimeCacheRoot) {
    return null;
  }
  return path.join(runtimeCacheRoot, ...packageNameSegments(platformPackage));
}

function packageCacheDir(
  runtimeCacheRoot,
  platformPackage,
  packageVersion,
  manifestDigest = null,
) {
  if (!runtimeCacheRoot || !packageVersion) {
    return null;
  }
  const cacheBaseDir = packageCacheBaseDir(runtimeCacheRoot, platformPackage);
  if (!cacheBaseDir) {
    return null;
  }
  const cacheKey = manifestDigest
    ? `${packageVersion}-${manifestDigest.slice(0, 12)}`
    : packageVersion;
  return path.join(cacheBaseDir, cacheKey);
}

export function resolveRuntimeCacheRoot({
  env = process.env,
  homeDir = (() => {
    try {
      return os.homedir();
    } catch {
      return null;
    }
  })(),
  platformName = process.platform,
} = {}) {
  const explicitCacheDir =
    env.LAVILAS_CODEX_VENDOR_CACHE_DIR ?? env.CODEX_VENDOR_CACHE_DIR ?? null;
  if (explicitCacheDir) {
    return path.resolve(explicitCacheDir);
  }

  const codexHome =
    env.CODEX_HOME ?? (homeDir ? path.join(homeDir, ".codex") : null);
  if (codexHome) {
    return path.join(codexHome, "runtime", "npm");
  }

  if (!homeDir) {
    return null;
  }

  switch (platformName) {
    case "win32": {
      const localAppData = env.LOCALAPPDATA || path.join(homeDir, "AppData", "Local");
      return path.join(localAppData, "Lavilas", "Codex", "runtime", "npm");
    }
    case "darwin":
      return path.join(homeDir, "Library", "Caches", "Lavilas", "Codex", "runtime", "npm");
    default: {
      const xdgCacheHome = env.XDG_CACHE_HOME || path.join(homeDir, ".cache");
      return path.join(xdgCacheHome, "lavilas-codex", "runtime", "npm");
    }
  }
}

function readJsonFile(filePath) {
  try {
    if (!existsSync(filePath)) {
      return null;
    }
    return JSON.parse(readFileSync(filePath, "utf8"));
  } catch {
    return null;
  }
}

function vendorManifestFor(vendorRoot) {
  return readJsonFile(path.join(vendorRoot, "manifest.json"));
}

function vendorManifestDigest(vendorRoot) {
  try {
    const manifestContents = readFileSync(path.join(vendorRoot, "manifest.json"));
    return createHash("sha256").update(manifestContents).digest("hex");
  } catch {
    return null;
  }
}

function readyMarkerPathFor(vendorRoot) {
  return path.join(path.dirname(vendorRoot), ".ready.json");
}

function readyMarkerFor(vendorRoot) {
  return readJsonFile(readyMarkerPathFor(vendorRoot));
}

function validateVendorManifest(manifest, vendorRoot) {
  if (!manifest || typeof manifest !== "object") {
    return { valid: false, reason: "missing manifest.json" };
  }

  const files = manifest.files;
  if (!files || typeof files !== "object") {
    return { valid: false, reason: "invalid manifest.json files map" };
  }

  for (const [relativePath, metadata] of Object.entries(files)) {
    const candidatePath = path.join(vendorRoot, relativePath);
    if (!existsSync(candidatePath)) {
      return { valid: false, reason: `missing ${relativePath}` };
    }

    let candidateStat;
    try {
      candidateStat = statSync(candidatePath);
    } catch {
      return { valid: false, reason: `unable to stat ${relativePath}` };
    }

    if (!candidateStat.isFile() || candidateStat.size <= 0) {
      return { valid: false, reason: `invalid ${relativePath}` };
    }

    if (
      metadata &&
      typeof metadata === "object" &&
      typeof metadata.size === "number" &&
      metadata.size !== candidateStat.size
    ) {
      return { valid: false, reason: `size mismatch for ${relativePath}` };
    }
  }

  return { valid: true };
}

function validateVendorRoot(
  candidate,
  targetTriple,
  binaryName,
  { requireReadyMarker = false } = {},
) {
  const manifest = vendorManifestFor(candidate.vendorRoot);
  const manifestValidation = validateVendorManifest(manifest, candidate.vendorRoot);
  if (!manifestValidation.valid) {
    return manifestValidation;
  }

  if (requireReadyMarker) {
    const readyMarker = readyMarkerFor(candidate.vendorRoot);
    if (!readyMarker || typeof readyMarker !== "object") {
      return { valid: false, reason: "missing .ready.json" };
    }
    if (
      candidate.cacheKey &&
      typeof readyMarker.cacheKey === "string" &&
      readyMarker.cacheKey !== candidate.cacheKey
    ) {
      return { valid: false, reason: "runtime cache marker mismatch" };
    }
  }

  const binaryRelativePath = `${targetTriple}/codex/${binaryName}`;
  const binaryPath = path.join(candidate.vendorRoot, binaryRelativePath);
  if (!existsSync(binaryPath)) {
    return { valid: false, reason: `missing ${binaryRelativePath}` };
  }

  let binaryStat;
  try {
    binaryStat = statSync(binaryPath);
  } catch {
    return { valid: false, reason: `unable to stat ${binaryRelativePath}` };
  }
  if (!binaryStat.isFile() || binaryStat.size <= 0) {
    return { valid: false, reason: `invalid ${binaryRelativePath}` };
  }

  const expectedBinary = manifest?.files?.[binaryRelativePath];
  if (
    expectedBinary &&
    typeof expectedBinary.size === "number" &&
    expectedBinary.size !== binaryStat.size
  ) {
    return {
      valid: false,
      reason: `size mismatch for ${binaryRelativePath}`,
    };
  }

  return {
    valid: true,
    binaryPath,
    pathDir: path.join(candidate.vendorRoot, targetTriple, "path"),
    vendorRoot: candidate.vendorRoot,
    source: candidate.source,
  };
}

function pushCandidate(candidates, seen, vendorRoot, source) {
  if (!existsSync(vendorRoot)) {
    return;
  }
  const resolvedRoot = path.resolve(vendorRoot);
  if (seen.has(resolvedRoot)) {
    return;
  }
  seen.add(resolvedRoot);
  candidates.push({ vendorRoot: resolvedRoot, source });
}

function copyDirectoryRecursive(sourceDir, destinationDir) {
  mkdirSync(destinationDir, { recursive: true });

  for (const entry of readdirSync(sourceDir, { withFileTypes: true })) {
    const sourcePath = path.join(sourceDir, entry.name);
    const destinationPath = path.join(destinationDir, entry.name);

    if (entry.isDirectory()) {
      copyDirectoryRecursive(sourcePath, destinationPath);
      continue;
    }

    if (entry.isSymbolicLink()) {
      const resolvedPath = realpathSync(sourcePath);
      const resolvedStat = statSync(resolvedPath);
      if (resolvedStat.isDirectory()) {
        copyDirectoryRecursive(resolvedPath, destinationPath);
      } else {
        copyFileSync(resolvedPath, destinationPath);
        chmodSync(destinationPath, resolvedStat.mode);
      }
      continue;
    }

    const sourceStat = lstatSync(sourcePath);
    if (sourceStat.isDirectory()) {
      copyDirectoryRecursive(sourcePath, destinationPath);
      continue;
    }

    copyFileSync(sourcePath, destinationPath);
    chmodSync(destinationPath, sourceStat.mode);
  }
}

function runtimeCacheCandidate({
  runtimeCacheRoot,
  platformPackage,
  packageVersion,
  manifestDigest = null,
  source = "runtime-cache",
}) {
  const cacheDir = packageCacheDir(
    runtimeCacheRoot,
    platformPackage,
    packageVersion,
    manifestDigest,
  );
  if (!cacheDir) {
    return null;
  }
  return {
    vendorRoot: path.join(cacheDir, "vendor"),
    cacheKey: path.basename(cacheDir),
    source,
  };
}

function collectRuntimeCacheCandidates({
  runtimeCacheRoot,
  platformPackage,
  packageVersion,
}) {
  const cacheBaseDir = packageCacheBaseDir(runtimeCacheRoot, platformPackage);
  if (!cacheBaseDir || !packageVersion || !existsSync(cacheBaseDir)) {
    return [];
  }

  const versionPrefix = `${packageVersion}-`;
  return readdirSync(cacheBaseDir, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name.startsWith(versionPrefix))
    .map((entry) => ({
      vendorRoot: path.join(cacheBaseDir, entry.name, "vendor"),
      cacheKey: entry.name,
      source: "runtime-cache",
    }));
}

function materializeRuntimeCache({
  runtimeCacheRoot,
  platformPackage,
  packageVersion,
  targetTriple,
  binaryName,
  sourceInstallation,
}) {
  const manifestDigest = vendorManifestDigest(sourceInstallation.vendorRoot);
  const cacheDir = packageCacheDir(
    runtimeCacheRoot,
    platformPackage,
    packageVersion,
    manifestDigest,
  );
  if (!cacheDir || !manifestDigest) {
    return null;
  }

  const cachedCandidate = runtimeCacheCandidate({
    runtimeCacheRoot,
    platformPackage,
    packageVersion,
    manifestDigest,
    source: `runtime-cache:${sourceInstallation.source}`,
  });
  if (!cachedCandidate) {
    return null;
  }

  const cachedInstallation = validateVendorRoot(
    cachedCandidate,
    targetTriple,
    binaryName,
    { requireReadyMarker: true },
  );
  if (cachedInstallation.valid) {
    return cachedInstallation;
  }

  const cacheParentDir = path.dirname(cacheDir);
  mkdirSync(cacheParentDir, { recursive: true });

  const tempCacheDir = path.join(
    cacheParentDir,
    `.tmp-${packageVersion}-${process.pid}-${Date.now()}-${Math.random()
      .toString(16)
      .slice(2)}`,
  );

  try {
    copyDirectoryRecursive(sourceInstallation.vendorRoot, path.join(tempCacheDir, "vendor"));

    const stagedInstallation = validateVendorRoot(
      {
        vendorRoot: path.join(tempCacheDir, "vendor"),
        cacheKey: path.basename(cacheDir),
        source: `runtime-cache-staging:${sourceInstallation.source}`,
      },
      targetTriple,
      binaryName,
    );
    if (!stagedInstallation.valid) {
      return null;
    }

    const readyMarker = {
      cacheKey: path.basename(cacheDir),
      platformPackage,
      packageVersion,
      targetTriple,
      manifestSha256: manifestDigest,
      createdAt: new Date().toISOString(),
    };
    writeFileSync(
      readyMarkerPathFor(path.join(tempCacheDir, "vendor")),
      `${JSON.stringify(readyMarker, null, 2)}\n`,
    );

    try {
      renameSync(tempCacheDir, cacheDir);
    } catch {
      const currentInstallation = validateVendorRoot(
        cachedCandidate,
        targetTriple,
        binaryName,
        { requireReadyMarker: true },
      );
      if (currentInstallation.valid) {
        return currentInstallation;
      }

      const currentReadyMarker = readyMarkerFor(cachedCandidate.vendorRoot);
      if (!currentReadyMarker && existsSync(cacheDir)) {
        rmSync(cacheDir, { recursive: true, force: true });
        renameSync(tempCacheDir, cacheDir);
      } else {
        return null;
      }
    }

    const finalInstallation = validateVendorRoot(
      cachedCandidate,
      targetTriple,
      binaryName,
      { requireReadyMarker: true },
    );
    return finalInstallation.valid ? finalInstallation : null;
  } catch {
    return null;
  } finally {
    rmSync(tempCacheDir, { recursive: true, force: true });
  }
}

export function collectVendorCandidates({
  packageDir,
  platformPackage,
  localVendorRoot = path.join(packageDir, "vendor"),
  requireResolve = null,
}) {
  const candidates = [];
  const seen = new Set();

  if (typeof requireResolve === "function") {
    try {
      const packageJsonPath = requireResolve(`${platformPackage}/package.json`);
      pushCandidate(
        candidates,
        seen,
        path.join(path.dirname(packageJsonPath), "vendor"),
        "resolved-platform-package",
      );
    } catch {
      // Ignore and continue with direct filesystem fallbacks.
    }
  }

  pushCandidate(
    candidates,
    seen,
    path.join(packageInstallDir(packageDir, platformPackage), "vendor"),
    "nested-platform-package",
  );

  const scopeDir = path.resolve(packageDir, "..");
  if (existsSync(scopeDir)) {
    for (const entry of readdirSync(scopeDir, { withFileTypes: true })) {
      if (!entry.isDirectory() || !entry.name.startsWith(".codex-")) {
        continue;
      }
      pushCandidate(
        candidates,
        seen,
        path.join(scopeDir, entry.name, "node_modules", ...packageNameSegments(platformPackage), "vendor"),
        `staged-platform-package:${entry.name}`,
      );
    }
  }

  pushCandidate(candidates, seen, localVendorRoot, "local-vendor-root");

  return candidates;
}

export function selectVendorInstallation({
  packageDir,
  platformPackage,
  targetTriple,
  binaryName,
  packageVersion = null,
  runtimeCacheRoot = null,
  localVendorRoot = path.join(packageDir, "vendor"),
  requireResolve = null,
}) {
  const candidates = collectVendorCandidates({
    packageDir,
    platformPackage,
    localVendorRoot,
    requireResolve,
  });
  for (const candidate of candidates) {
    const result = validateVendorRoot(candidate, targetTriple, binaryName);
    if (result.valid) {
      return (
        materializeRuntimeCache({
          runtimeCacheRoot,
          platformPackage,
          packageVersion,
          targetTriple,
          binaryName,
          sourceInstallation: result,
        }) ?? result
      );
    }
  }

  for (const cachedCandidate of collectRuntimeCacheCandidates({
    runtimeCacheRoot,
    platformPackage,
    packageVersion,
  })) {
    const cachedInstallation = validateVendorRoot(
      cachedCandidate,
      targetTriple,
      binaryName,
      { requireReadyMarker: true },
    );
    if (cachedInstallation.valid) {
      return cachedInstallation;
    }
  }

  return null;
}

export function buildPlatformPackageMetadata({
  platformPackage,
  version,
  license = "Apache-2.0",
  repository = null,
  packageManager = null,
}) {
  const metadata = {
    name: platformPackage,
    version,
    license,
    files: ["vendor"],
  };
  if (repository) {
    metadata.repository = repository;
  }
  if (packageManager) {
    metadata.packageManager = packageManager;
  }

  if (platformPackage.includes("linux")) {
    metadata.os = ["linux"];
  } else if (platformPackage.includes("darwin")) {
    metadata.os = ["darwin"];
  } else if (platformPackage.includes("win32")) {
    metadata.os = ["win32"];
  }

  if (platformPackage.endsWith("x64")) {
    metadata.cpu = ["x64"];
  } else if (platformPackage.endsWith("arm64")) {
    metadata.cpu = ["arm64"];
  }

  metadata.engines = { node: ">=16" };
  return metadata;
}

export function ensurePlatformPackageMetadata({
  packageDir,
  platformPackage,
  version,
  license = "Apache-2.0",
  repository = null,
  packageManager = null,
}) {
  const vendorRoot = path.join(packageDir, "vendor");
  if (!existsSync(vendorRoot)) {
    return null;
  }

  const packageJsonPath = path.join(packageDir, "package.json");
  if (existsSync(packageJsonPath)) {
    return packageJsonPath;
  }

  mkdirSync(packageDir, { recursive: true });
  const metadata = buildPlatformPackageMetadata({
    platformPackage,
    version,
    license,
    repository,
    packageManager,
  });
  writeFileSync(packageJsonPath, `${JSON.stringify(metadata, null, 2)}\n`);
  return packageJsonPath;
}
