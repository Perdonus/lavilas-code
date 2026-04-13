import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  statSync,
  writeFileSync,
} from "node:fs";
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

function validateVendorRoot(candidate, targetTriple, binaryName) {
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

  const manifest = vendorManifestFor(candidate.vendorRoot);
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
      return result;
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
