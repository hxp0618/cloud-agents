import { createHash } from "node:crypto";
import { lstatSync, readFileSync, readdirSync, realpathSync, statSync } from "node:fs";
import { isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const TOOLS_ROOT = "/opt/cloud-agents-worker-tools";
const PACKAGE_MANIFEST = `${TOOLS_ROOT}/package.json`;
const PACKAGE_LOCK = `${TOOLS_ROOT}/package-lock.json`;
const HIDDEN_PACKAGE_LOCK = `${TOOLS_ROOT}/node_modules/.package-lock.json`;
const STATIC_MANIFEST = "/usr/share/doc/cloud-agents/worker-oci-install-manifest.json";
const DPKG_STATUS = "/var/lib/dpkg/status";
const DEBIAN_DOC_ROOT = "/usr/share/doc";
const COMMON_LICENSE_ROOT = "/usr/share/common-licenses";
const COLLECTOR_PATH = fileURLToPath(import.meta.url);
const LICENSE_BASENAME = /^(?:licen[cs]e|copying|copyright|notice)(?:[._-].*)?$/iu;
const SUPPLEMENTAL_LICENSE_ROOT = "/usr/share/doc/cloud-agents/worker-oci-supplemental-licenses";
const MAX_LICENSE_FILE_BYTES = 1_048_576;
const MAX_PACKAGE_FILE_BYTES = 64 * 1024 * 1024;

type Platform = "linux/amd64" | "linux/arm64";
type CheckStatus = "PASS" | "BLOCKED";

type RuntimeIdentity = Readonly<{
  platform: string;
  arch: string;
}>;

type JsonRecord = Record<string, unknown>;

type LockPackage = Readonly<{
  name?: unknown;
  version?: unknown;
  resolved?: unknown;
  integrity?: unknown;
  license?: unknown;
  cpu?: unknown;
  os?: unknown;
}>;

type LicenseFile = Readonly<{
  path: string;
  resolvedPath: string;
  sha256: string;
  size: number;
}>;

type PackageFile = Readonly<{
  path: string;
  sha256: string;
  size: number;
}>;

type SupplementalLicenseSource = Readonly<
  | {
      kind: "repository";
      repository: string;
      commit: string;
      path: string;
    }
  | {
      kind: "npm-tarball";
      url: string;
      integrity: string;
      member: string;
    }
  | {
      kind: "source-archive";
      url: string;
      sha256: string;
      member: string;
    }
>;

type SupplementalLicenseAuthority = Readonly<{
  id: string;
  artifactPath: string;
  sha256: string;
  imagePath: string;
  source: SupplementalLicenseSource;
}>;

type PackageBinding = Readonly<{
  path: string;
  name: string;
  version: string;
  integrity: string;
  supplementalLicenseTextIds: ReadonlyArray<string>;
  bundledLicenseFiles: ReadonlyArray<Readonly<{ path: string; sha256: string }>>;
  packageFiles?: ReadonlyArray<PackageFile>;
}>;

type BoundSupplementalLicenseText = Readonly<{
  id: string;
  path: string;
  sha256: string;
  source: SupplementalLicenseSource;
  packagePaths: ReadonlyArray<string>;
}>;

export type WorkerOciInstalledInventory = Readonly<{
  schemaVersion: 1;
  kind: "cloud-agents-worker-oci-installed-inventory";
  platform: Platform;
  status: "BLOCKED";
  packageRecordConsistency: "PASS";
  licenseArtifactPresence: CheckStatus;
  source: Readonly<{
    packageManifest: typeof PACKAGE_MANIFEST;
    packageManifestSha256: string;
    packageLock: typeof PACKAGE_LOCK;
    packageLockSha256: string;
    hiddenPackageLock: typeof HIDDEN_PACKAGE_LOCK;
    hiddenPackageLockSha256: string;
    staticManifest: typeof STATIC_MANIFEST;
    staticManifestSha256: string;
    installedInventoryCollector: string;
    installedInventoryCollectorSha256: string;
    dpkgStatus: typeof DPKG_STATUS;
    dpkgStatusSha256: string;
  }>;
  npm: Readonly<{
    status: CheckStatus;
    packages: ReadonlyArray<
      Readonly<{
        physicalPath: string;
        name: string;
        version: string;
        resolved: string;
        integrity: string;
        declaredLicense: string;
        packageJSONSha256: string;
        licenseArtifactPresence: CheckStatus;
        licenseFiles: ReadonlyArray<LicenseFile>;
        packageContentVerification: "PASS" | "NOT_RUN";
        packageFiles: ReadonlyArray<PackageFile> | null;
      }>
    >;
  }>;
  debian: Readonly<{
    status: CheckStatus;
    packages: ReadonlyArray<
      Readonly<{
        package: string;
        version: string;
        architecture: string;
        source: string | "NOASSERTION";
        copyright: LicenseFile | null;
      }>
    >;
    commonLicenses: ReadonlyArray<LicenseFile>;
  }>;
  blockedReasons: ReadonlyArray<string>;
  fullSupplyChain: Readonly<{
    status: "BLOCKED";
    limits: ReadonlyArray<string>;
  }>;
  supplementalLicenseTexts: ReadonlyArray<BoundSupplementalLicenseText>;
}>;

/**
 * Captures the installed Worker dependency inventory without changing the image.
 * Structural or integrity drift throws. Missing license artifacts remain observable as
 * a deterministic BLOCKED result so callers can retain the otherwise valid census.
 */
export function captureWorkerOciInstalledInventory(
  root = "/",
  platform: Platform,
  runtime: RuntimeIdentity = { platform: process.platform, arch: process.arch },
): WorkerOciInstalledInventory {
  const rootPath = realpathSync(resolve(root));
  const architecture = platform === "linux/arm64" ? "arm64" : "amd64";
  const runtimeArchitecture = architecture === "amd64" ? "x64" : "arm64";
  if (runtime.platform !== "linux" || runtime.arch !== runtimeArchitecture) {
    throw new Error(
      `expected ${platform}, but runtime architecture is ${runtime.platform}/${runtime.arch}`,
    );
  }

  const packageManifestBytes = readVirtualFile(rootPath, PACKAGE_MANIFEST);
  const packageLockBytes = readVirtualFile(rootPath, PACKAGE_LOCK);
  const hiddenLockBytes = readVirtualFile(rootPath, HIDDEN_PACKAGE_LOCK);
  const staticManifestBytes = readVirtualFile(rootPath, STATIC_MANIFEST);
  const dpkgStatusBytes = readVirtualFile(rootPath, DPKG_STATUS);
  const collectorBytes = readFileSync(COLLECTOR_PATH);
  const staticManifest = parseObject(staticManifestBytes, STATIC_MANIFEST);
  validateStaticManifest(staticManifest, packageManifestBytes, packageLockBytes, collectorBytes);
  const supplementalLicenseTexts = parseSupplementalLicenseTexts(staticManifest);

  const packageManifest = parseObject(packageManifestBytes, PACKAGE_MANIFEST);
  if (packageManifest.private !== true) {
    throw new Error(`${PACKAGE_MANIFEST} must be a private package manifest`);
  }
  const rootLock = parseLock(packageLockBytes, PACKAGE_LOCK);
  const packageBindings = parsePackageBindings(staticManifest, supplementalLicenseTexts, rootLock);
  const hiddenLock = parseLock(hiddenLockBytes, HIDDEN_PACKAGE_LOCK);
  const actualPackagePaths = enumeratePhysicalPackageRoots(rootPath, `${TOOLS_ROOT}/node_modules`);
  const hiddenPackagePaths = Object.keys(hiddenLock).toSorted(compareText);
  assertSamePackageSet(actualPackagePaths, hiddenPackagePaths);
  validateCurrentPackageFileBindings(
    packageBindings,
    rootLock,
    actualPackagePaths,
    runtimeArchitecture,
  );
  if (actualPackagePaths.length === 0) throw new Error("installed npm inventory is empty");
  const requiredDependencies = recordField(
    packageManifest.dependencies,
    `${PACKAGE_MANIFEST} dependencies`,
  );
  for (const [name, version] of Object.entries(requiredDependencies)) {
    requiredString(version, `${PACKAGE_MANIFEST} dependency ${name}`);
    if (!actualPackagePaths.includes(`node_modules/${name}`)) {
      throw new Error(`required direct dependency ${name} is not installed`);
    }
  }

  const blockedReasons: string[] = [];
  const npmPackages = actualPackagePaths.map((physicalPath) => {
    validatePackageLockPath(physicalPath);
    const rootEntry = requireLockEntry(rootLock, physicalPath, PACKAGE_LOCK);
    const hiddenEntry = requireLockEntry(hiddenLock, physicalPath, HIDDEN_PACKAGE_LOCK);
    validateLockPair(physicalPath, rootEntry, hiddenEntry);
    validatePlatformConstraint(physicalPath, "os", rootEntry.os, "linux");
    validatePlatformConstraint(physicalPath, "cpu", rootEntry.cpu, runtimeArchitecture);

    const packageDirectory = virtualToHost(rootPath, `${TOOLS_ROOT}/${physicalPath}`);
    const packageJsonPath = join(packageDirectory, "package.json");
    const packageJsonStat = lstatSync(packageJsonPath);
    if (packageJsonStat.isSymbolicLink()) {
      throw new Error(`${physicalPath} package.json is a symbolic link`);
    }
    if (!packageJsonStat.isFile()) {
      throw new Error(`${physicalPath} package.json is not a regular file`);
    }
    const packageJsonBytes = readFileSync(packageJsonPath);
    const packageJson = parseObject(packageJsonBytes, `${TOOLS_ROOT}/${physicalPath}/package.json`);
    const physicalName = packageNameFromPath(physicalPath);
    const expectedName =
      optionalString(rootEntry.name, `${physicalPath} lock name`) ?? physicalName;
    const actualName = requiredString(packageJson.name, `${physicalPath} package.json name`);
    const version = requiredString(rootEntry.version, `${physicalPath} lock version`);
    if (actualName !== expectedName) {
      throw new Error(
        `${physicalPath} package.json name ${actualName} does not match lock name ${expectedName}`,
      );
    }
    if (requiredString(packageJson.version, `${physicalPath} package.json version`) !== version) {
      throw new Error(`${physicalPath} package.json version does not match the lock`);
    }
    const declaredLicense =
      optionalString(packageJson.license, `${physicalPath} package.json license`) ?? "NOASSERTION";
    const binding = packageBindings.get(physicalPath);
    if (binding !== undefined) {
      validateInstalledLicenseBinding(physicalPath, actualName, version, rootEntry, binding);
    }
    const licenseFiles = collectNpmLicenseFiles(rootPath, packageDirectory, declaredLicense);
    if (binding !== undefined) {
      licenseFiles.push(
        ...collectBundledLicenseFiles(rootPath, packageDirectory, physicalPath, binding),
      );
    }
    const packageFiles =
      binding?.packageFiles === undefined
        ? null
        : collectBoundPackageFiles(packageDirectory, physicalPath, binding.packageFiles);
    const deduplicatedLicenseFiles = deduplicateLicenseFiles(licenseFiles);
    const licenseIssues = npmLicenseIssues(physicalPath, declaredLicense, deduplicatedLicenseFiles);
    blockedReasons.push(...licenseIssues);
    return {
      physicalPath,
      name: actualName,
      version,
      resolved: requiredString(rootEntry.resolved, `${physicalPath} lock resolved`),
      integrity: requiredString(rootEntry.integrity, `${physicalPath} lock integrity`),
      declaredLicense,
      packageJSONSha256: sha256(packageJsonBytes),
      licenseArtifactPresence: licenseIssues.length === 0 ? "PASS" : "BLOCKED",
      licenseFiles: deduplicatedLicenseFiles,
      packageContentVerification: binding?.packageFiles === undefined ? "NOT_RUN" : "PASS",
      packageFiles,
    } as const;
  });

  const installedPackagePaths = new Set(npmPackages.map(({ physicalPath }) => physicalPath));
  const installedSupplementalLicenseTexts = collectInstalledSupplementalLicenseTexts(
    rootPath,
    supplementalLicenseTexts,
    packageBindings,
    installedPackagePaths,
  );

  const commonLicenses = collectLicenseTree(
    rootPath,
    COMMON_LICENSE_ROOT,
    [COMMON_LICENSE_ROOT],
    false,
  );
  const debianPackages = parseDpkgStatus(dpkgStatusBytes).map((record) => {
    if (record.architecture !== "all" && record.architecture !== architecture) {
      throw new Error(
        `Debian package ${record.package} has architecture ${record.architecture}; expected ${architecture} or all`,
      );
    }
    const copyrightPath = `${DEBIAN_DOC_ROOT}/${record.package}/copyright`;
    const copyright = collectSingleLicenseFile(
      rootPath,
      copyrightPath,
      [DEBIAN_DOC_ROOT, COMMON_LICENSE_ROOT],
      true,
    );
    if (copyright === null) {
      blockedReasons.push(`Debian package ${record.package} has no ${copyrightPath} file.`);
    }
    return { ...record, copyright };
  });

  const npmBlocked = npmPackages.some(
    ({ licenseArtifactPresence }) => licenseArtifactPresence === "BLOCKED",
  );
  const debianBlocked = debianPackages.some(({ copyright }) => copyright === null);
  const licenseArtifactPresence = npmBlocked || debianBlocked ? "BLOCKED" : "PASS";
  return {
    schemaVersion: 1,
    kind: "cloud-agents-worker-oci-installed-inventory",
    platform,
    status: "BLOCKED",
    packageRecordConsistency: "PASS",
    licenseArtifactPresence,
    source: {
      packageManifest: PACKAGE_MANIFEST,
      packageManifestSha256: sha256(packageManifestBytes),
      packageLock: PACKAGE_LOCK,
      packageLockSha256: sha256(packageLockBytes),
      hiddenPackageLock: HIDDEN_PACKAGE_LOCK,
      hiddenPackageLockSha256: sha256(hiddenLockBytes),
      staticManifest: STATIC_MANIFEST,
      staticManifestSha256: sha256(staticManifestBytes),
      installedInventoryCollector: "/usr/share/doc/cloud-agents/worker-oci-installed.ts",
      installedInventoryCollectorSha256: sha256(collectorBytes),
      dpkgStatus: DPKG_STATUS,
      dpkgStatusSha256: sha256(dpkgStatusBytes),
    },
    npm: {
      status: npmBlocked ? "BLOCKED" : "PASS",
      packages: npmPackages,
    },
    debian: {
      status: debianBlocked ? "BLOCKED" : "PASS",
      packages: debianPackages,
      commonLicenses,
    },
    blockedReasons,
    fullSupplyChain: {
      status: "BLOCKED",
      limits: [
        "License artifact presence is filename-based and does not prove license meaning, completeness, or legal approval.",
        "Installed file hashes do not prove npm tarball extraction completeness or executable provenance.",
        "Apt download bytes and repository metadata are not retained or bound.",
        "The base image, standalone Runtime, and vendored native libraries are not a complete license closure.",
        "This inventory is not a legal approval or a signature for the final published image.",
      ],
    },
    supplementalLicenseTexts: installedSupplementalLicenseTexts,
  };
}

function validateStaticManifest(
  manifest: JsonRecord,
  packageManifestBytes: Buffer,
  packageLockBytes: Buffer,
  collectorBytes: Buffer,
): void {
  if (
    manifest.schemaVersion !== 2 ||
    manifest.kind !== "cloud-agents-worker-oci-install-manifest"
  ) {
    throw new Error(`${STATIC_MANIFEST} has an unsupported contract`);
  }
  const source = recordField(manifest.source, `${STATIC_MANIFEST} source`);
  const bindings = [
    ["packageManifestSha256", sha256(packageManifestBytes), "package.json"],
    ["lockfileSha256", sha256(packageLockBytes), "package-lock.json"],
    ["installedInventoryCollectorSha256", sha256(collectorBytes), "installed inventory collector"],
  ] as const;
  for (const [field, actual, description] of bindings) {
    if (source[field] !== actual) {
      throw new Error(`static manifest ${description} binding does not match installed bytes`);
    }
  }
}

function parseSupplementalLicenseTexts(
  manifest: JsonRecord,
): ReadonlyMap<string, SupplementalLicenseAuthority> {
  const values = manifest.supplementalLicenseTexts;
  if (!Array.isArray(values)) {
    throw new Error(`${STATIC_MANIFEST} supplementalLicenseTexts must be an array`);
  }
  const result = new Map<string, SupplementalLicenseAuthority>();
  const artifactPaths = new Set<string>();
  const imagePaths = new Set<string>();
  for (const [index, value] of values.entries()) {
    const label = `${STATIC_MANIFEST} supplementalLicenseTexts[${index}]`;
    const item = recordField(value, label);
    const id = requiredString(item.id, `${label} id`);
    if (result.has(id))
      throw new Error(`${STATIC_MANIFEST} duplicate supplemental license text id ${id}`);
    const artifactPath = requiredSupplementalArtifactPath(
      item.artifactPath,
      `${label} artifactPath`,
    );
    if (artifactPaths.has(artifactPath)) {
      throw new Error(
        `${STATIC_MANIFEST} duplicate supplemental license artifact path ${artifactPath}`,
      );
    }
    artifactPaths.add(artifactPath);
    const sha256Value = requiredSha256(item.sha256, `${label} sha256`);
    const imagePath = requiredSupplementalImagePath(item.imagePath, `${label} imagePath`);
    if (imagePaths.has(imagePath)) {
      throw new Error(`${STATIC_MANIFEST} duplicate supplemental license image path ${imagePath}`);
    }
    imagePaths.add(imagePath);
    result.set(id, {
      id,
      artifactPath,
      sha256: sha256Value,
      imagePath,
      source: parseSupplementalSource(item.source, `${label} source`),
    });
  }
  return result;
}

function parsePackageBindings(
  manifest: JsonRecord,
  supplementalLicenseTexts: ReadonlyMap<string, SupplementalLicenseAuthority>,
  rootLock: Record<string, LockPackage>,
): ReadonlyMap<string, PackageBinding> {
  if (manifest.licenseBindings !== undefined) {
    throw new Error(
      `${STATIC_MANIFEST} legacy licenseBindings is unsupported; use packageBindings`,
    );
  }
  const values = manifest.packageBindings;
  if (!Array.isArray(values)) {
    throw new Error(`${STATIC_MANIFEST} packageBindings must be an array`);
  }
  const result = new Map<string, PackageBinding>();
  for (const [index, value] of values.entries()) {
    const label = `${STATIC_MANIFEST} packageBindings[${index}]`;
    const item = recordField(value, label);
    const path = requiredString(item.path, `${label} path`);
    validatePackageLockPath(path);
    const lockEntry = rootLock[path];
    if (lockEntry === undefined) {
      throw new Error(`${label} path ${path} has no package-lock.json record`);
    }
    if (result.has(path))
      throw new Error(`${STATIC_MANIFEST} duplicate package binding path ${path}`);
    const expectedName =
      optionalString(lockEntry.name, `${path} lock name`) ?? packageNameFromPath(path);
    const name = requiredString(item.name, `${label} name`);
    if (name !== expectedName) {
      throw new Error(
        `${label} name ${name} does not match package-lock.json name ${expectedName}`,
      );
    }
    const version = requiredString(item.version, `${label} version`);
    const lockVersion = requiredString(lockEntry.version, `${path} lock version`);
    if (version !== lockVersion) {
      throw new Error(
        `${label} version ${version} does not match package-lock.json version ${lockVersion}`,
      );
    }
    const integrity = requiredString(item.integrity, `${label} integrity`);
    const lockIntegrity = requiredString(lockEntry.integrity, `${path} lock integrity`);
    if (integrity !== lockIntegrity) {
      throw new Error(`${label} integrity does not match package-lock.json`);
    }
    const supplementalIds = parseSupplementalIds(
      item.supplementalLicenseTextIds,
      supplementalLicenseTexts,
      label,
    );
    const bundledLicenseFiles = parseBundledLicenseFiles(item.bundledLicenseFiles, label);
    const packageFiles = parsePackageFiles(item.packageFiles, label);
    result.set(path, {
      path,
      name,
      version,
      integrity,
      supplementalLicenseTextIds: supplementalIds,
      bundledLicenseFiles,
      ...(packageFiles === undefined ? {} : { packageFiles }),
    });
  }
  return result;
}

function parseSupplementalIds(
  value: unknown,
  supplementalLicenseTexts: ReadonlyMap<string, SupplementalLicenseAuthority>,
  label: string,
): string[] {
  if (!Array.isArray(value))
    throw new Error(`${label} supplementalLicenseTextIds must be an array`);
  const result: string[] = [];
  const seen = new Set<string>();
  for (const [index, entry] of value.entries()) {
    const id = requiredString(entry, `${label} supplementalLicenseTextIds[${index}]`);
    if (seen.has(id)) throw new Error(`${label} has duplicate supplemental license text id ${id}`);
    if (!supplementalLicenseTexts.has(id)) {
      throw new Error(`${label} references unknown supplemental license text id ${id}`);
    }
    seen.add(id);
    result.push(id);
  }
  return result.toSorted(compareText);
}

function parseBundledLicenseFiles(
  value: unknown,
  label: string,
): Array<Readonly<{ path: string; sha256: string }>> {
  if (!Array.isArray(value)) throw new Error(`${label} bundledLicenseFiles must be an array`);
  const result: Array<Readonly<{ path: string; sha256: string }>> = [];
  const seen = new Set<string>();
  for (const [index, entry] of value.entries()) {
    const item = recordField(entry, `${label} bundledLicenseFiles[${index}]`);
    const path = requiredSafeRelativeString(
      item.path,
      `${label} bundledLicenseFiles[${index}] path`,
    );
    if (seen.has(path)) throw new Error(`${label} has duplicate bundled license path ${path}`);
    seen.add(path);
    result.push({
      path,
      sha256: requiredSha256(item.sha256, `${label} bundledLicenseFiles[${index}] sha256`),
    });
  }
  return result.toSorted((left, right) => compareText(left.path, right.path));
}

function parsePackageFiles(value: unknown, label: string): PackageFile[] | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value)) throw new Error(`${label} packageFiles must be an array`);
  if (value.length === 0) throw new Error(`${label} packageFiles must not be empty`);
  const result: PackageFile[] = [];
  const seen = new Set<string>();
  for (const [index, entry] of value.entries()) {
    const item = recordField(entry, `${label} packageFiles[${index}]`);
    const path = requiredSafeRelativeString(item.path, `${label} packageFiles[${index}] path`);
    if (seen.has(path)) throw new Error(`${label} has duplicate package file path ${path}`);
    seen.add(path);
    result.push({
      path,
      sha256: requiredSha256(item.sha256, `${label} packageFiles[${index}] sha256`),
      size: requiredPackageFileSize(item.size, `${label} packageFiles[${index}] size`),
    });
  }
  if (!seen.has("package.json")) {
    throw new Error(`${label} packageFiles must contain package.json`);
  }
  for (const path of seen) {
    const parts = path.split("/");
    for (let length = 1; length < parts.length; length += 1) {
      const ancestor = parts.slice(0, length).join("/");
      if (seen.has(ancestor)) {
        throw new Error(`${label} packageFiles has an ancestor collision at ${ancestor}`);
      }
    }
  }
  return result.toSorted((left, right) => compareText(left.path, right.path));
}

function parseSupplementalSource(value: unknown, label: string): SupplementalLicenseSource {
  const source = recordField(value, label);
  const kind = requiredString(source.kind, `${label} kind`);
  if (kind === "repository") {
    const commit = requiredString(source.commit, `${label} commit`);
    if (!/^[0-9a-f]{40}$/iu.test(commit)) {
      throw new Error(`${label} commit must be a 40-character hexadecimal revision`);
    }
    return {
      kind,
      repository: requiredHttpsUrl(source.repository, `${label} repository`),
      commit,
      path: requiredSafeRelativeString(source.path, `${label} path`),
    };
  }
  if (kind === "npm-tarball") {
    return {
      kind,
      url: requiredHttpsUrl(source.url, `${label} url`),
      integrity: requiredSri(source.integrity, `${label} integrity`),
      member: requiredSafeRelativeString(source.member, `${label} member`),
    };
  }
  if (kind === "source-archive") {
    return {
      kind,
      url: requiredHttpsUrl(source.url, `${label} url`),
      sha256: requiredSha256(source.sha256, `${label} sha256`),
      member: requiredSafeRelativeString(source.member, `${label} member`),
    };
  }
  throw new Error(`${label} kind ${kind} is unsupported`);
}

function validateInstalledLicenseBinding(
  physicalPath: string,
  actualName: string,
  version: string,
  rootEntry: LockPackage,
  binding: PackageBinding,
): void {
  if (binding.path !== physicalPath) {
    throw new Error(`${physicalPath} package binding path does not match installed package path`);
  }
  const lockName =
    optionalString(rootEntry.name, `${physicalPath} lock name`) ??
    packageNameFromPath(physicalPath);
  if (binding.name !== actualName || binding.name !== lockName) {
    throw new Error(`${physicalPath} package binding name does not match installed package`);
  }
  if (binding.version !== version) {
    throw new Error(`${physicalPath} package binding version does not match installed package`);
  }
  if (binding.integrity !== requiredString(rootEntry.integrity, `${physicalPath} lock integrity`)) {
    throw new Error(`${physicalPath} package binding integrity does not match installed package`);
  }
}

function collectBundledLicenseFiles(
  root: string,
  packageDirectory: string,
  physicalPath: string,
  binding: PackageBinding,
): LicenseFile[] {
  return binding.bundledLicenseFiles.map(({ path, sha256: expectedHash }) => {
    const virtualPath = `${hostToVirtual(root, packageDirectory)}/${path}`;
    const file = collectExactLicenseFile(
      root,
      virtualPath,
      [hostToVirtual(root, packageDirectory)],
      true,
    );
    if (file === null) {
      throw new Error(`${physicalPath} bundled license file ${path} is missing`);
    }
    if (file.sha256 !== expectedHash) {
      throw new Error(
        `${physicalPath} bundled license file ${path} sha256 does not match authority`,
      );
    }
    return {
      ...file,
      path,
      resolvedPath: file.resolvedPath,
    };
  });
}

function collectBoundPackageFiles(
  packageDirectory: string,
  physicalPath: string,
  expected: ReadonlyArray<PackageFile>,
): PackageFile[] {
  const observed = enumeratePackageFiles(packageDirectory, physicalPath);
  const expectedByPath = new Map(expected.map((file) => [file.path, file]));
  const observedByPath = new Map(observed.map((file) => [file.path, file]));
  const missing = expected.filter(({ path }) => !observedByPath.has(path)).map(({ path }) => path);
  const extra = observed.filter(({ path }) => !expectedByPath.has(path)).map(({ path }) => path);
  if (missing.length > 0 || extra.length > 0) {
    throw new Error(
      `${physicalPath} package file set does not match authority; missing=[${missing.join(", ")}], extra=[${extra.join(", ")}]`,
    );
  }
  for (const file of expected) {
    const actual = observedByPath.get(file.path)!;
    if (actual.size !== file.size) {
      throw new Error(`${physicalPath} package file ${file.path} size does not match authority`);
    }
    if (actual.sha256 !== file.sha256) {
      throw new Error(`${physicalPath} package file ${file.path} sha256 does not match authority`);
    }
  }
  return observed;
}

function enumeratePackageFiles(packageDirectory: string, physicalPath: string): PackageFile[] {
  const result: PackageFile[] = [];
  const visit = (directory: string, prefix: string): void => {
    for (const entry of readdirSync(directory).toSorted(compareText)) {
      const child = join(directory, entry);
      const path = prefix === "" ? entry : `${prefix}/${entry}`;
      if (!isSafeRelativePath(path)) {
        throw new Error(`${physicalPath} package file ${path} is not a safe relative path`);
      }
      const childStat = lstatSync(child);
      if (childStat.isSymbolicLink()) {
        throw new Error(`${physicalPath} package file ${path} is a symbolic link`);
      }
      if (childStat.isDirectory()) {
        visit(child, path);
        continue;
      }
      if (!childStat.isFile()) {
        throw new Error(`${physicalPath} package file ${path} is not a regular file`);
      }
      if (childStat.size > MAX_PACKAGE_FILE_BYTES) {
        throw new Error(
          `${physicalPath} package file ${path} exceeds the ${MAX_PACKAGE_FILE_BYTES}-byte package file limit`,
        );
      }
      const bytes = readFileSync(child);
      if (bytes.length > MAX_PACKAGE_FILE_BYTES) {
        throw new Error(
          `${physicalPath} package file ${path} exceeds the ${MAX_PACKAGE_FILE_BYTES}-byte package file limit`,
        );
      }
      result.push({ path, sha256: sha256(bytes), size: bytes.length });
    }
  };
  visit(packageDirectory, "");
  return result.toSorted((left, right) => compareText(left.path, right.path));
}

function collectInstalledSupplementalLicenseTexts(
  root: string,
  supplementalLicenseTexts: ReadonlyMap<string, SupplementalLicenseAuthority>,
  packageBindings: ReadonlyMap<string, PackageBinding>,
  installedPackagePaths: ReadonlySet<string>,
): BoundSupplementalLicenseText[] {
  const result: BoundSupplementalLicenseText[] = [];
  for (const [id, authority] of [...supplementalLicenseTexts.entries()].toSorted(
    ([left], [right]) => compareText(left, right),
  )) {
    const packagePaths = [...packageBindings.values()]
      .filter(
        (binding) =>
          installedPackagePaths.has(binding.path) &&
          binding.supplementalLicenseTextIds.includes(id),
      )
      .map(({ path }) => path)
      .toSorted(compareText);
    if (packagePaths.length === 0) continue;
    const file = collectSupplementalLicenseFile(root, authority);
    result.push({
      id,
      path: authority.imagePath,
      sha256: file.sha256,
      source: authority.source,
      packagePaths,
    });
  }
  return result;
}

function collectSupplementalLicenseFile(
  root: string,
  authority: SupplementalLicenseAuthority,
): LicenseFile {
  const file = collectExactLicenseFile(
    root,
    authority.imagePath,
    [SUPPLEMENTAL_LICENSE_ROOT],
    true,
  );
  if (file === null) {
    throw new Error(`supplemental license text ${authority.id} is missing`);
  }
  if (file.sha256 !== authority.sha256) {
    throw new Error(`supplemental license text ${authority.id} sha256 does not match authority`);
  }
  return file;
}

function collectExactLicenseFile(
  root: string,
  virtualPath: string,
  allowedRoots: string[],
  missingIsNull: boolean,
): LicenseFile | null {
  const hostPath = virtualToHost(root, virtualPath);
  let stat;
  try {
    stat = lstatSync(hostPath);
  } catch (error) {
    if (missingIsNull && isMissing(error)) return null;
    throw error;
  }
  if (stat.isSymbolicLink()) throw new Error(`${virtualPath} is a symbolic link`);
  if (!stat.isFile()) throw new Error(`${virtualPath} is not a regular license file`);
  return collectSingleLicenseFile(root, virtualPath, allowedRoots, false);
}

function deduplicateLicenseFiles(files: LicenseFile[]): LicenseFile[] {
  const result = new Map<string, LicenseFile>();
  for (const file of files) {
    const existing = result.get(file.path);
    if (existing !== undefined && existing.sha256 !== file.sha256) {
      throw new Error(`license file ${file.path} is bound to conflicting bytes`);
    }
    result.set(file.path, file);
  }
  return [...result.values()].toSorted((left, right) => compareText(left.path, right.path));
}

function parseLock(source: Buffer, label: string): Record<string, LockPackage> {
  const parsed = parseObject(source, label);
  if (parsed.lockfileVersion !== 3) {
    throw new Error(`${label} must use lockfileVersion 3`);
  }
  const packages = recordField(parsed.packages, `${label} packages`);
  const result: Record<string, LockPackage> = {};
  for (const [path, value] of Object.entries(packages)) {
    if (path === "") continue;
    validatePackageLockPath(path);
    result[path] = recordField(value, `${label} ${path}`);
  }
  return result;
}

function enumeratePhysicalPackageRoots(root: string, nodeModulesPath: string): string[] {
  const result: string[] = [];
  const visitNodeModules = (virtualNodeModules: string): void => {
    const hostNodeModules = virtualToHost(root, virtualNodeModules);
    const nodeModulesStat = lstatSync(hostNodeModules);
    if (nodeModulesStat.isSymbolicLink()) {
      throw new Error(`${virtualNodeModules} is a symbolic link`);
    }
    if (!nodeModulesStat.isDirectory()) {
      throw new Error(`${virtualNodeModules} is not a directory`);
    }
    for (const entry of readdirSync(hostNodeModules).toSorted(compareText)) {
      if (entry.startsWith(".")) continue;
      const entryVirtual = `${virtualNodeModules}/${entry}`;
      const entryHost = virtualToHost(root, entryVirtual);
      const entryStat = lstatSync(entryHost);
      if (entryStat.isSymbolicLink()) {
        throw new Error(`npm package root candidate ${entryVirtual} is a symbolic link`);
      }
      if (!entryStat.isDirectory()) continue;
      if (entry.startsWith("@")) {
        for (const scopedEntry of readdirSync(entryHost).toSorted(compareText)) {
          if (scopedEntry.startsWith(".")) continue;
          const packageVirtual = `${entryVirtual}/${scopedEntry}`;
          collectPackage(packageVirtual);
        }
      } else {
        collectPackage(entryVirtual);
      }
    }
  };
  const collectPackage = (packageVirtual: string): void => {
    const packageHost = virtualToHost(root, packageVirtual);
    const packageStat = lstatSync(packageHost);
    if (packageStat.isSymbolicLink()) {
      throw new Error(`npm package root ${packageVirtual} is a symbolic link`);
    }
    if (!packageStat.isDirectory()) {
      throw new Error(`npm package root ${packageVirtual} is not a directory`);
    }
    const relativePath = relative(virtualToHost(root, TOOLS_ROOT), packageHost)
      .split(sep)
      .join("/");
    result.push(relativePath);
    const nested = join(packageHost, "node_modules");
    try {
      const nestedStat = lstatSync(nested);
      if (nestedStat.isSymbolicLink()) {
        throw new Error(`${packageVirtual}/node_modules is a symbolic link`);
      }
      if (nestedStat.isDirectory()) {
        visitNodeModules(`${packageVirtual}/node_modules`);
      }
    } catch (error) {
      if (!isMissing(error)) throw error;
    }
  };
  visitNodeModules(nodeModulesPath);
  return result.toSorted(compareText);
}

function assertSamePackageSet(actual: string[], expected: string[]): void {
  const actualSet = new Set(actual);
  const expectedSet = new Set(expected);
  const missing = expected.filter((path) => !actualSet.has(path));
  const extra = actual.filter((path) => !expectedSet.has(path));
  if (missing.length > 0 || extra.length > 0) {
    throw new Error(
      `installed npm package set does not match hidden lock; missing=[${missing.join(", ")}], extra=[${extra.join(", ")}]`,
    );
  }
}

function validateCurrentPackageFileBindings(
  packageBindings: ReadonlyMap<string, PackageBinding>,
  rootLock: Record<string, LockPackage>,
  installedPackagePaths: ReadonlyArray<string>,
  runtimeArchitecture: string,
): void {
  const installed = new Set(installedPackagePaths);
  for (const binding of packageBindings.values()) {
    if (binding.packageFiles === undefined) continue;
    const lockEntry = requireLockEntry(rootLock, binding.path, PACKAGE_LOCK);
    const appliesToLinux = platformConstraintApplies(binding.path, "os", lockEntry.os, "linux");
    const appliesToRuntime = platformConstraintApplies(
      binding.path,
      "cpu",
      lockEntry.cpu,
      runtimeArchitecture,
    );
    if (appliesToLinux && appliesToRuntime && !installed.has(binding.path)) {
      throw new Error(
        `${binding.path} packageFiles binding applies to the current platform but is not installed`,
      );
    }
  }
}

function validatePackageLockPath(path: string): void {
  if (
    path.startsWith("/") ||
    path.includes("\\") ||
    path.split("/").some((part) => part === "" || part === "." || part === "..") ||
    !/(?:^|\/)node_modules\/(?:@[^/]+\/)?[^/]+$/u.test(path)
  ) {
    throw new Error(`invalid npm package lock path ${path}`);
  }
}

function validateLockPair(path: string, root: LockPackage, hidden: LockPackage): void {
  for (const field of ["version", "resolved", "integrity"] as const) {
    const rootValue = requiredString(root[field], `${path} root lock ${field}`);
    const hiddenValue = requiredString(hidden[field], `${path} hidden lock ${field}`);
    if (rootValue !== hiddenValue) {
      throw new Error(`${path} hidden lock ${field} does not match root lock`);
    }
  }
  const rootName = optionalString(root.name, `${path} root lock name`);
  const hiddenName = optionalString(hidden.name, `${path} hidden lock name`);
  if (rootName !== hiddenName) {
    throw new Error(`${path} hidden lock name does not match root lock`);
  }
}

function validatePlatformConstraint(
  path: string,
  field: "cpu" | "os",
  value: unknown,
  expected: string,
): void {
  if (!platformConstraintApplies(path, field, value, expected)) {
    throw new Error(`${path} does not allow ${field} ${expected}`);
  }
}

function platformConstraintApplies(
  path: string,
  field: "cpu" | "os",
  value: unknown,
  expected: string,
): boolean {
  if (value === undefined) return true;
  if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) {
    throw new Error(`${path} ${field} constraint must be a string array`);
  }
  const constraints = value as string[];
  const positive = constraints.filter((item) => !item.startsWith("!"));
  return (
    !constraints.includes(`!${expected}`) && (positive.length === 0 || positive.includes(expected))
  );
}

function collectNpmLicenseFiles(
  root: string,
  packageDirectory: string,
  declaredLicense: string,
): LicenseFile[] {
  const packageVirtual = hostToVirtual(root, packageDirectory);
  const files = collectLicenseTree(root, packageVirtual, [packageVirtual], true);
  const named = /^SEE LICENSE IN (.+)$/u.exec(declaredLicense)?.[1];
  if (named !== undefined) {
    if (!isSafeRelativePath(named)) {
      throw new Error(`${packageVirtual} has an unsafe SEE LICENSE IN path ${named}`);
    }
    const virtualPath = `${packageVirtual}/${named}`;
    const exact = collectSingleLicenseFile(root, virtualPath, [packageVirtual], true);
    if (exact !== null && !files.some(({ path }) => path === virtualPath)) files.push(exact);
  }
  return files
    .map((file) => ({
      ...file,
      path: relative(packageDirectory, virtualToHost(root, file.path)).split(sep).join("/"),
      resolvedPath: relative(packageDirectory, virtualToHost(root, file.resolvedPath))
        .split(sep)
        .join("/"),
    }))
    .toSorted((left, right) => compareText(left.path, right.path));
}

function npmLicenseIssues(
  physicalPath: string,
  declaredLicense: string,
  licenseFiles: LicenseFile[],
): string[] {
  if (declaredLicense === "NOASSERTION" || declaredLicense === "UNLICENSED") {
    return [`npm ${physicalPath} has no usable declared license.`];
  }
  const named = /^SEE LICENSE IN (.+)$/u.exec(declaredLicense)?.[1];
  if (named !== undefined && !licenseFiles.some(({ path }) => path === named)) {
    return [`npm ${physicalPath} declares SEE LICENSE IN ${named} but that file is missing.`];
  }
  return licenseFiles.length === 0
    ? [`npm ${physicalPath} has no installed license, notice, copying, or copyright file.`]
    : [];
}

function collectLicenseTree(
  root: string,
  virtualDirectory: string,
  allowedRoots: string[],
  namesOnly: boolean,
): LicenseFile[] {
  const hostDirectory = virtualToHost(root, virtualDirectory);
  const files: LicenseFile[] = [];
  const visit = (hostPath: string): void => {
    for (const entry of readdirSync(hostPath).toSorted(compareText)) {
      if (entry === "node_modules") continue;
      const child = join(hostPath, entry);
      const childStat = lstatSync(child);
      if (childStat.isDirectory()) {
        visit(child);
      } else if (!namesOnly || LICENSE_BASENAME.test(entry)) {
        const file = collectSingleLicenseFile(
          root,
          hostToVirtual(root, child),
          allowedRoots,
          false,
        );
        if (file !== null) files.push(file);
      }
    }
  };
  visit(hostDirectory);
  return files.toSorted((left, right) => compareText(left.path, right.path));
}

function collectSingleLicenseFile(
  root: string,
  virtualPath: string,
  allowedRoots: string[],
  missingIsNull: boolean,
): LicenseFile | null {
  const hostPath = virtualToHost(root, virtualPath);
  let resolvedPath: string;
  try {
    resolvedPath = realpathSync(hostPath);
  } catch (error) {
    if (missingIsNull && isMissing(error)) return null;
    throw error;
  }
  if (!allowedRoots.some((allowed) => isWithin(virtualToHost(root, allowed), resolvedPath))) {
    throw new Error(`${virtualPath} resolves outside its allowed license roots`);
  }
  if (!statSync(resolvedPath).isFile()) {
    throw new Error(`${virtualPath} is not a regular license file`);
  }
  const bytes = readFileSync(resolvedPath);
  if (bytes.length > MAX_LICENSE_FILE_BYTES) {
    throw new Error(`${virtualPath} exceeds the ${MAX_LICENSE_FILE_BYTES}-byte license file limit`);
  }
  return {
    path: virtualPath,
    resolvedPath: hostToVirtual(root, resolvedPath),
    sha256: sha256(bytes),
    size: bytes.length,
  };
}

function parseDpkgStatus(source: Buffer): Array<{
  package: string;
  version: string;
  architecture: string;
  source: string | "NOASSERTION";
}> {
  const result = [];
  const identities = new Set<string>();
  for (const paragraph of source.toString("utf8").split(/\n\s*\n/u)) {
    if (paragraph.trim() === "") continue;
    const fields: Record<string, string> = {};
    let current: string | undefined;
    for (const line of paragraph.split("\n")) {
      if (line === "") continue;
      if (/^[ \t]/u.test(line)) {
        if (current !== undefined) fields[current] += `\n${line}`;
        continue;
      }
      const separator = line.indexOf(":");
      if (separator <= 0) throw new Error(`${DPKG_STATUS} contains a malformed field`);
      current = line.slice(0, separator);
      fields[current] = line.slice(separator + 1).trim();
    }
    const status = requiredString(fields.Status, "Debian package status").split(" ");
    if (status.length !== 3) {
      throw new Error(`${DPKG_STATUS} contains a malformed package status`);
    }
    const [selection, errorState, currentState] = status;
    if (currentState !== "installed") continue;
    if ((selection !== "install" && selection !== "hold") || errorState !== "ok") {
      throw new Error(`${DPKG_STATUS} contains a broken installed state: ${fields.Status}`);
    }
    const packageName = requiredString(fields.Package, "installed Debian package name");
    if (!/^[a-z0-9][a-z0-9+.-]*$/u.test(packageName)) {
      throw new Error(`invalid Debian package name ${packageName}`);
    }
    const architecture = requiredString(
      fields.Architecture,
      "installed Debian package architecture",
    );
    const identity = `${packageName}:${architecture}`;
    if (identities.has(identity)) {
      throw new Error(`duplicate installed Debian package ${identity}`);
    }
    identities.add(identity);
    result.push({
      package: packageName,
      version: requiredString(fields.Version, "installed Debian package version"),
      architecture,
      source: fields.Source ?? "NOASSERTION",
    });
  }
  if (result.length === 0) throw new Error(`${DPKG_STATUS} has no installed package records`);
  return result.toSorted((left, right) =>
    compareText(`${left.package}:${left.architecture}`, `${right.package}:${right.architecture}`),
  );
}

function packageNameFromPath(path: string): string {
  const parts = path.split("/");
  const lastNodeModules = parts.lastIndexOf("node_modules");
  const first = parts[lastNodeModules + 1]!;
  return first.startsWith("@") ? `${first}/${parts[lastNodeModules + 2]!}` : first;
}

function requireLockEntry(
  lock: Record<string, LockPackage>,
  path: string,
  label: string,
): LockPackage {
  const entry = lock[path];
  if (entry === undefined) throw new Error(`${label} has no ${path} record`);
  return entry;
}

function parseObject(source: Buffer, label: string): JsonRecord {
  let parsed: unknown;
  try {
    parsed = JSON.parse(source.toString("utf8"));
  } catch {
    throw new Error(`${label} is not valid JSON`);
  }
  return recordField(parsed, label);
}

function recordField(value: unknown, label: string): JsonRecord {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  return value as JsonRecord;
}

function requiredString(value: unknown, label: string): string {
  if (typeof value !== "string" || value === "") throw new Error(`${label} must be a string`);
  return value;
}

function requiredSafeRelativeString(value: unknown, label: string): string {
  const path = requiredString(value, label);
  if (!isSafeRelativePath(path)) throw new Error(`${label} must be a safe relative path`);
  return path;
}

function requiredSha256(value: unknown, label: string): string {
  const digest = requiredString(value, label);
  if (!/^[0-9a-f]{64}$/u.test(digest))
    throw new Error(`${label} must be a lowercase SHA-256 digest`);
  return digest;
}

function requiredPackageFileSize(value: unknown, label: string): number {
  if (
    typeof value !== "number" ||
    !Number.isSafeInteger(value) ||
    value < 0 ||
    value > MAX_PACKAGE_FILE_BYTES
  ) {
    throw new Error(`${label} must be an integer between 0 and ${MAX_PACKAGE_FILE_BYTES}`);
  }
  return value;
}

function requiredSri(value: unknown, label: string): string {
  const integrity = requiredString(value, label);
  if (!isCanonicalSha512Sri(integrity)) throw new Error(`${label} must be canonical SHA-512 SRI`);
  return integrity;
}

// The collector is copied into the Worker image as a standalone Node script, so it cannot
// import the build-time supply generator. Keep the same canonical SHA-512 SRI rule at this
// runtime boundary instead of accepting a weaker tarball source assertion.
function isCanonicalSha512Sri(value: string): boolean {
  const match = /^sha512-(?<digest>[A-Za-z0-9+/]+={0,2})$/u.exec(value);
  if (!match?.groups?.digest) return false;
  const decoded = Buffer.from(match.groups.digest, "base64");
  return decoded.length === 64 && decoded.toString("base64") === match.groups.digest;
}

function requiredHttpsUrl(value: unknown, label: string): string {
  const valueString = requiredString(value, label);
  let url: URL;
  try {
    url = new URL(valueString);
  } catch {
    throw new Error(`${label} must be an HTTPS URL`);
  }
  if (url.protocol !== "https:" || url.username || url.password || url.hash) {
    throw new Error(`${label} must be an HTTPS URL`);
  }
  return valueString;
}

function requiredSupplementalImagePath(value: unknown, label: string): string {
  const imagePath = requiredString(value, label);
  const prefix = `${SUPPLEMENTAL_LICENSE_ROOT}/`;
  if (!imagePath.startsWith(prefix)) {
    throw new Error(`${label} must be under ${SUPPLEMENTAL_LICENSE_ROOT}`);
  }
  const suffix = imagePath.slice(prefix.length);
  if (!isSafeRelativePath(suffix)) throw new Error(`${label} must have a safe relative suffix`);
  return imagePath;
}

function requiredSupplementalArtifactPath(value: unknown, label: string): string {
  const artifactPath = requiredString(value, label);
  const prefix = "deploy/docker/worker-oci-supplemental-licenses/";
  if (!artifactPath.startsWith(prefix)) {
    throw new Error(`${label} must be under ${prefix}`);
  }
  if (!isSafeRelativePath(artifactPath.slice(prefix.length))) {
    throw new Error(`${label} must have a safe relative suffix`);
  }
  return artifactPath;
}

function optionalString(value: unknown, label: string): string | undefined {
  if (value === undefined) return undefined;
  return requiredString(value, label);
}

function readVirtualFile(root: string, virtualPath: string): Buffer {
  const hostPath = virtualToHost(root, virtualPath);
  const fileStat = lstatSync(hostPath);
  if (fileStat.isSymbolicLink()) throw new Error(`${virtualPath} is a symbolic link`);
  if (!fileStat.isFile()) throw new Error(`${virtualPath} is not a regular file`);
  const resolvedPath = realpathSync(hostPath);
  if (!isWithin(root, resolvedPath)) throw new Error(`${virtualPath} escapes inventory root`);
  return readFileSync(resolvedPath);
}

function virtualToHost(root: string, virtualPath: string): string {
  if (!virtualPath.startsWith("/") || virtualPath.includes("\\")) {
    throw new Error(`invalid absolute inventory path ${virtualPath}`);
  }
  const hostPath = resolve(root, virtualPath.slice(1));
  if (!isWithin(root, hostPath)) throw new Error(`${virtualPath} escapes inventory root`);
  return hostPath;
}

function hostToVirtual(root: string, hostPath: string): string {
  if (!isWithin(root, hostPath)) throw new Error(`${hostPath} escapes inventory root`);
  const path = relative(root, hostPath).split(sep).join("/");
  return `/${path}`;
}

function isWithin(parent: string, child: string): boolean {
  const path = relative(resolve(parent), resolve(child));
  return path === "" || (!path.startsWith(`..${sep}`) && path !== ".." && !isAbsolute(path));
}

function isSafeRelativePath(path: string): boolean {
  return (
    path !== "" &&
    !isAbsolute(path) &&
    !path.includes("\\") &&
    !/[\u0000-\u001f\u007f]/u.test(path) &&
    !path.split("/").some((part) => part === "" || part === "." || part === "..")
  );
}

function isMissing(error: unknown): boolean {
  return (
    error !== null &&
    typeof error === "object" &&
    "code" in error &&
    (error as { code?: unknown }).code === "ENOENT"
  );
}

function sha256(source: Buffer): string {
  return createHash("sha256").update(source).digest("hex");
}

function compareText(left: string, right: string): number {
  return left < right ? -1 : left > right ? 1 : 0;
}

function runCli(): void {
  const platform = process.argv[2];
  if (platform !== "linux/amd64" && platform !== "linux/arm64") {
    throw new Error("usage: node worker-oci-installed.ts linux/amd64|linux/arm64");
  }
  process.stdout.write(
    `${JSON.stringify(captureWorkerOciInstalledInventory("/", platform), null, 2)}\n`,
  );
}

if (process.argv[1] !== undefined && resolve(process.argv[1]) === resolve(COLLECTOR_PATH)) {
  runCli();
}
