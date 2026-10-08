import { createHash } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { join, relative } from "node:path";

const WORKER_DOCKERFILE = "deploy/docker/worker.Dockerfile";
const PACKAGE_MANIFEST = "deploy/docker/worker-tools/package.json";
const PACKAGE_LOCK = "deploy/docker/worker-tools/package-lock.json";
const PACKAGE_AUTHORITY = "deploy/docker/worker-oci-package-authority.json";
const INSTALLED_INVENTORY_COLLECTOR = "scripts/lib/worker-oci-installed.ts";
const SUPPLEMENTAL_ARTIFACT_PREFIX = "deploy/docker/worker-oci-supplemental-licenses/";
const SUPPLEMENTAL_IMAGE_PREFIX = "/usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/";
const MAX_PACKAGE_FILE_BYTES = 64 * 1024 * 1024;
const NOASSERTION = "NOASSERTION" as const;

export const WORKER_OCI_INSTALL_MANIFEST_FILENAME = "cloud-agents-worker-oci-install-manifest.json";
export const WORKER_OCI_NOTICES_FILENAME = "cloud-agents-worker-oci-notices.md";
export const WORKER_OCI_INSTALL_MANIFEST_ARCHIVE_PATH =
  "deploy/docker/worker-oci-install-manifest.json";
export const WORKER_OCI_NOTICES_ARCHIVE_PATH = "deploy/docker/worker-oci-notices.md";

type NpmPackageRecord = Readonly<{
  name?: unknown;
  version?: unknown;
  resolved?: unknown;
  integrity?: unknown;
  license?: unknown;
  cpu?: unknown;
}>;

type NpmLock = Readonly<{
  rootDependencies: Record<string, string>;
  packages: Record<string, NpmPackageRecord>;
}>;

export type WorkerOciSupplementalSource = Readonly<
  | {
      readonly kind: "repository";
      readonly repository: string;
      readonly commit: string;
      readonly path: string;
    }
  | {
      readonly kind: "npm-tarball";
      readonly url: string;
      readonly integrity: string;
      readonly member: string;
    }
  | {
      readonly kind: "source-archive";
      readonly url: string;
      readonly sha256: string;
      readonly member: string;
    }
>;

export type WorkerOciSupplementalLicenseText = Readonly<{
  readonly id: string;
  readonly artifactPath: string;
  readonly sha256: string;
  readonly source: WorkerOciSupplementalSource;
}>;

export type WorkerOciBundledLicenseFile = Readonly<{
  readonly path: string;
  readonly sha256: string;
}>;

export type WorkerOciPackageFile = Readonly<{
  readonly path: string;
  readonly sha256: string;
  readonly size: number;
}>;

export type WorkerOciPackageAuthorityEntry = Readonly<{
  readonly package: string;
  readonly path: string;
  readonly version: string;
  readonly sourceUrl: string;
  readonly integrity: string;
  readonly license: string;
  readonly supplementalLicenseTextIds: ReadonlyArray<string>;
  readonly bundledLicenseFiles: ReadonlyArray<WorkerOciBundledLicenseFile>;
  readonly packageFiles?: ReadonlyArray<WorkerOciPackageFile>;
}>;

type WorkerOciPackageAuthority = Readonly<{
  readonly registry: string;
  readonly blockedReasons: ReadonlyArray<string>;
  readonly packages: ReadonlyMap<string, WorkerOciPackageAuthorityEntry>;
  readonly supplementalLicenseTexts: ReadonlyMap<string, WorkerOciSupplementalLicenseText>;
}>;

type DirectInstallResolution = Readonly<{
  readonly installs: ReadonlyArray<WorkerOciDirectInstall>;
  readonly blockedReasons: ReadonlyArray<string>;
}>;

export type WorkerOciDirectInstall = Readonly<{
  readonly package: string;
  readonly version: string;
  readonly architectures: ReadonlyArray<"linux/amd64" | "linux/arm64">;
  readonly sourceUrl: string | typeof NOASSERTION;
  readonly integrity: string | typeof NOASSERTION;
  readonly license: string;
}>;

export type WorkerOciPackageBinding = Readonly<{
  readonly path: string;
  readonly name: string;
  readonly version: string;
  readonly integrity: string;
  readonly supplementalLicenseTextIds: ReadonlyArray<string>;
  readonly bundledLicenseFiles: ReadonlyArray<WorkerOciBundledLicenseFile>;
  readonly packageFiles?: ReadonlyArray<WorkerOciPackageFile>;
}>;

export type WorkerOciInstallManifest = Readonly<{
  readonly schemaVersion: 2;
  readonly kind: "cloud-agents-worker-oci-install-manifest";
  readonly status: "PASS" | "BLOCKED";
  readonly source: Readonly<{
    readonly dockerfile: string;
    readonly dockerfileSha256: string;
    readonly packageManifest: string;
    readonly packageManifestSha256: string;
    readonly lockfile: string;
    readonly lockfileSha256: string;
    readonly packageAuthority: string;
    readonly packageAuthoritySha256: string;
    readonly installedInventoryCollector: string;
    readonly installedInventoryCollectorSha256: string;
  }>;
  readonly registry: string | typeof NOASSERTION;
  readonly baseImage: Readonly<{
    readonly reference: string;
    readonly digest: string | typeof NOASSERTION;
    readonly license: typeof NOASSERTION;
  }>;
  readonly osInstall: ReadonlyArray<
    Readonly<{ readonly package: string; readonly license: typeof NOASSERTION }>
  >;
  readonly directInstalls: ReadonlyArray<WorkerOciDirectInstall>;
  readonly supplementalLicenseTexts: ReadonlyArray<
    WorkerOciSupplementalLicenseText & { readonly imagePath: string }
  >;
  readonly packageBindings: ReadonlyArray<WorkerOciPackageBinding>;
  readonly blockedReasons: ReadonlyArray<string>;
  readonly transitiveClosure: "NOT_PROVEN";
}>;

export type WorkerOciSupplyArtifacts = Readonly<{
  readonly manifest: WorkerOciInstallManifest;
  readonly manifestBytes: Buffer;
  readonly noticeBytes: Buffer;
  readonly sourceFiles: ReadonlyMap<string, Buffer>;
}>;

export function buildWorkerOciSupplyArtifacts(root: string): WorkerOciSupplyArtifacts {
  const fixedSourcePaths = [
    WORKER_DOCKERFILE,
    PACKAGE_MANIFEST,
    PACKAGE_LOCK,
    PACKAGE_AUTHORITY,
    INSTALLED_INVENTORY_COLLECTOR,
  ];
  const sourceFiles = new Map(
    fixedSourcePaths
      .toSorted()
      .map((path): readonly [string, Buffer] => [path, readRegularSource(root, path)]),
  );
  const dockerfileSource = sourceFiles.get(WORKER_DOCKERFILE)!;
  const packageManifestSource = sourceFiles.get(PACKAGE_MANIFEST)!;
  const lockfileSource = sourceFiles.get(PACKAGE_LOCK)!;
  const packageAuthoritySource = sourceFiles.get(PACKAGE_AUTHORITY)!;
  const dockerfile = dockerfileSource.toString("utf8");
  const packageManifest = parsePackageManifest(packageManifestSource);
  const lock = parsePackageLock(lockfileSource);
  const packageAuthority = parsePackageAuthority(packageAuthoritySource, lock);
  for (const supplemental of [...packageAuthority.supplementalLicenseTexts.values()].toSorted(
    (left, right) => left.artifactPath.localeCompare(right.artifactPath),
  )) {
    const bytes = readRegularSource(root, supplemental.artifactPath);
    if (sha256(bytes) !== supplemental.sha256) {
      throw new Error(
        `Worker OCI supplemental license artifact ${supplemental.artifactPath} SHA-256 does not match authority.`,
      );
    }
    sourceFiles.set(supplemental.artifactPath, bytes);
  }
  const sortedSourceFiles = new Map(
    [...sourceFiles.entries()].toSorted(([left], [right]) => left.localeCompare(right)),
  );
  const registry = parseRegistry(dockerfile);
  const directInstallResolution = resolveDirectInstalls(
    packageManifest,
    lock,
    packageAuthority.packages,
  );
  const baseImage = parseBaseImage(dockerfile);
  const blockedReasons = [
    ...(packageAuthority.registry === registry
      ? []
      : [
          `Worker OCI package authority registry ${packageAuthority.registry} does not match Dockerfile registry ${registry}.`,
        ]),
    ...validateDockerfileLockInstall(dockerfile),
    ...validateLockRegistryAndSri(lock, registry),
    ...directInstallResolution.blockedReasons,
    ...(baseImage.digest === NOASSERTION
      ? ["Worker base image is tag-only; its immutable digest and license are not bound."]
      : []),
    "The apt package closure and license inventory are not bound by repository evidence.",
    "The npm lock binds fetched bytes, but the installed platform-specific closure and its license inventory are not proven.",
    ...packageAuthority.blockedReasons,
  ];
  const supplementalLicenseTexts = [...packageAuthority.supplementalLicenseTexts.values()]
    .toSorted((left, right) => left.id.localeCompare(right.id))
    .map((item) => ({
      ...item,
      imagePath: `${SUPPLEMENTAL_IMAGE_PREFIX}${item.artifactPath.slice(SUPPLEMENTAL_ARTIFACT_PREFIX.length)}`,
    }));
  const manifest: WorkerOciInstallManifest = {
    schemaVersion: 2,
    kind: "cloud-agents-worker-oci-install-manifest",
    status: blockedReasons.length === 0 ? "PASS" : "BLOCKED",
    source: {
      dockerfile: WORKER_DOCKERFILE,
      dockerfileSha256: sha256(dockerfileSource),
      packageManifest: PACKAGE_MANIFEST,
      packageManifestSha256: sha256(packageManifestSource),
      lockfile: PACKAGE_LOCK,
      lockfileSha256: sha256(lockfileSource),
      packageAuthority: PACKAGE_AUTHORITY,
      packageAuthoritySha256: sha256(packageAuthoritySource),
      installedInventoryCollector: INSTALLED_INVENTORY_COLLECTOR,
      installedInventoryCollectorSha256: sha256(
        sortedSourceFiles.get(INSTALLED_INVENTORY_COLLECTOR)!,
      ),
    },
    registry,
    baseImage: {
      reference: baseImage.reference,
      digest: baseImage.digest,
      license: NOASSERTION,
    },
    osInstall: parseOsInstall(dockerfile),
    directInstalls: directInstallResolution.installs,
    supplementalLicenseTexts,
    packageBindings: [...packageAuthority.packages.values()]
      .filter((item) => hasPackageBindingReferences(item))
      .toSorted((left, right) => left.path.localeCompare(right.path))
      .map((item) => ({
        path: item.path,
        name: item.package,
        version: item.version,
        integrity: item.integrity,
        supplementalLicenseTextIds: [...item.supplementalLicenseTextIds],
        bundledLicenseFiles: item.bundledLicenseFiles.map((file) => ({ ...file })),
        ...(item.packageFiles === undefined
          ? {}
          : { packageFiles: item.packageFiles.map((file) => ({ ...file })) }),
      })),
    blockedReasons,
    transitiveClosure: "NOT_PROVEN",
  };
  const manifestBytes = Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`);
  return {
    manifest,
    manifestBytes,
    noticeBytes: Buffer.from(renderNotice(manifest)),
    sourceFiles: sortedSourceFiles,
  };
}

export function renderWorkerOciNotice(manifest: WorkerOciInstallManifest): string {
  return renderNotice(manifest);
}

function resolveDirectInstalls(
  packageManifest: Record<string, string>,
  lock: NpmLock,
  packageAuthority: ReadonlyMap<string, WorkerOciPackageAuthorityEntry>,
): DirectInstallResolution {
  const installs: WorkerOciDirectInstall[] = [];
  const blockedReasons: string[] = [];
  if (!recordsEqual(packageManifest, lock.rootDependencies)) {
    blockedReasons.push(
      "package.json dependencies and optionalDependencies do not match package-lock.json root declarations.",
    );
  }
  for (const [packageName, version] of Object.entries(packageManifest)) {
    const path = `node_modules/${packageName}`;
    const entry = lock.packages[path];
    const authority = packageAuthority.get(path);
    const lockVersion = typeof entry?.version === "string" ? entry.version : undefined;
    const sourceUrl = typeof entry?.resolved === "string" ? entry.resolved : NOASSERTION;
    const integrity = typeof entry?.integrity === "string" ? entry.integrity : NOASSERTION;
    const license = typeof entry?.license === "string" ? entry.license : NOASSERTION;
    const integrityValid = isCanonicalSha512Sri(integrity);
    if (!entry || lockVersion !== version) {
      blockedReasons.push(`${packageName}@${version} has no exact package-lock.json record.`);
    }
    if (!integrityValid) {
      blockedReasons.push(
        `${packageName}@${version} package-lock.json integrity is not canonical SHA-512 SRI.`,
      );
    }
    if (!authority) {
      blockedReasons.push(
        `${packageName}@${version} is not bound by the Worker OCI package authority.`,
      );
    }
    const authorityMatches =
      authority !== undefined &&
      authority.package === packageName &&
      authority.path === path &&
      authority.version === version &&
      authority.sourceUrl === sourceUrl &&
      authority.integrity === integrity &&
      authority.license === license;
    if (authority && !authorityMatches) {
      blockedReasons.push(
        `${packageName}@${version} package authority does not match package-lock.json source URL, integrity, and license.`,
      );
    }
    const bound =
      entry !== undefined && lockVersion === version && integrityValid && authorityMatches;
    installs.push({
      package: packageName,
      version,
      architectures: architecturesFor(entry),
      sourceUrl: bound ? sourceUrl : NOASSERTION,
      integrity: bound ? integrity : NOASSERTION,
      license: bound ? license : NOASSERTION,
    });
  }
  for (const authority of packageAuthority.values()) {
    const direct =
      authority.path === `node_modules/${authority.package}` &&
      packageManifest[authority.package] === authority.version;
    if (!direct && !hasPackageBindingReferences(authority)) {
      blockedReasons.push(
        `${authority.path} package authority is not a direct package.json dependency and has no supplemental or bundled license reference.`,
      );
    }
  }
  return {
    installs: installs.toSorted((left, right) => left.package.localeCompare(right.package)),
    blockedReasons,
  };
}

function validateLockRegistryAndSri(
  lock: NpmLock,
  registry: string | typeof NOASSERTION,
): ReadonlyArray<string> {
  const blockedReasons: string[] = [];
  for (const [path, entry] of Object.entries(lock.packages)) {
    if (path === "") continue;
    const integrity = typeof entry.integrity === "string" ? entry.integrity : NOASSERTION;
    if (!isCanonicalSha512Sri(integrity)) {
      blockedReasons.push(`${PACKAGE_LOCK} ${path} integrity is not canonical SHA-512 SRI.`);
    }
    const sourceUrl = typeof entry.resolved === "string" ? entry.resolved : NOASSERTION;
    if (registry === NOASSERTION || !sourceUrl.startsWith(registry)) {
      blockedReasons.push(`${PACKAGE_LOCK} ${path} source URL is outside the configured registry.`);
    }
  }
  return blockedReasons;
}

function validateDockerfileLockInstall(dockerfile: string): ReadonlyArray<string> {
  const required = [
    "ADD cloud-agents-deployment-*.tar /deployment/",
    "COPY --from=lock-inputs /deployment/deploy/docker/worker-tools/package.json /opt/cloud-agents-worker-tools/package.json",
    "COPY --from=lock-inputs /deployment/deploy/docker/worker-tools/package-lock.json /opt/cloud-agents-worker-tools/package-lock.json",
    "npm ci --prefix /opt/cloud-agents-worker-tools",
    "--ignore-scripts",
    "--omit=dev",
  ];
  const blockedReasons = required.every((value) => dockerfile.includes(value))
    ? []
    : ["Worker Dockerfile does not consume the dedicated npm lock with npm ci --ignore-scripts."];
  if (
    ![
      "COPY --from=lock-inputs /deployment/scripts/lib/worker-oci-installed.ts /usr/share/doc/cloud-agents/worker-oci-installed.ts",
      'RUN node /usr/share/doc/cloud-agents/worker-oci-installed.ts "linux/${TARGETARCH}" > /usr/share/doc/cloud-agents/worker-oci-installed-inventory.json',
      "COPY --from=lock-inputs /deployment/deploy/docker/worker-oci-supplemental-licenses/ /usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/",
    ].every((value) => dockerfile.includes(value))
  ) {
    blockedReasons.push(
      "Worker Dockerfile does not capture the bound installed inventory and supplemental license materials.",
    );
  }
  return blockedReasons;
}

function parsePackageManifest(source: Buffer): Record<string, string> {
  const parsed: unknown = JSON.parse(source.toString("utf8"));
  if (!isRecord(parsed) || parsed.private !== true) {
    throw new Error("Worker tool package.json is invalid.");
  }
  return {
    ...requireStringRecord(parsed.dependencies, "dependencies"),
    ...requireStringRecord(parsed.optionalDependencies, "optionalDependencies"),
  };
}

function parsePackageLock(source: Buffer): NpmLock {
  const parsed: unknown = JSON.parse(source.toString("utf8"));
  if (!isRecord(parsed) || parsed.lockfileVersion !== 3 || !isRecord(parsed.packages)) {
    throw new Error("Worker tool package-lock.json must be npm lockfileVersion 3.");
  }
  const root = parsed.packages[""];
  if (!isRecord(root)) throw new Error("Worker tool package-lock.json root entry is invalid.");
  return {
    rootDependencies: {
      ...requireStringRecord(root.dependencies, "lock root dependencies"),
      ...requireStringRecord(root.optionalDependencies, "lock root optionalDependencies"),
    },
    packages: parsed.packages as Record<string, NpmPackageRecord>,
  };
}

function parsePackageAuthority(source: Buffer, lock: NpmLock): WorkerOciPackageAuthority {
  const parsed: unknown = JSON.parse(source.toString("utf8"));
  if (
    !isRecord(parsed) ||
    parsed.schemaVersion !== 2 ||
    !Array.isArray(parsed.packages) ||
    !Array.isArray(parsed.supplementalLicenseTexts)
  ) {
    throw new Error("Worker OCI package authority must use schemaVersion 2.");
  }
  const registry = requireString(parsed.registry, "registry");
  const blockedReasons = parseStringArray(parsed.blockedReasons, "blockedReasons");
  if (
    blockedReasons.some((reason) => reason.trim() === "" || /[\u0000-\u001f\u007f]/u.test(reason))
  ) {
    throw new Error(
      "Worker OCI package authority blockedReasons must be nonblank single-line text.",
    );
  }
  const supplementalLicenseTexts = new Map<string, WorkerOciSupplementalLicenseText>();
  const artifactPaths = new Set<string>();
  for (const item of parsed.supplementalLicenseTexts) {
    if (!isRecord(item)) throw new Error("Worker OCI supplemental license text entry is invalid.");
    const id = requireString(item.id, "supplemental license text id");
    const artifactPath = requireString(item.artifactPath, `${id} artifactPath`);
    if (!isSafeArtifactPath(artifactPath)) {
      throw new Error(`Worker OCI supplemental license artifact path ${artifactPath} is invalid.`);
    }
    if (supplementalLicenseTexts.has(id)) {
      throw new Error(`Worker OCI supplemental license text has duplicate ${id}.`);
    }
    if (artifactPaths.has(artifactPath)) {
      throw new Error(
        `Worker OCI supplemental license artifact path has duplicate ${artifactPath}.`,
      );
    }
    const supplemental: WorkerOciSupplementalLicenseText = {
      id,
      artifactPath,
      sha256: requireSha256(item.sha256, `${id} sha256`),
      source: parseSupplementalSource(item.source, id),
    };
    supplementalLicenseTexts.set(id, supplemental);
    artifactPaths.add(artifactPath);
  }
  const authority = new Map<string, WorkerOciPackageAuthorityEntry>();
  for (const item of parsed.packages) {
    if (!isRecord(item)) throw new Error("Worker OCI package authority entry is invalid.");
    const packageName = requireString(item.name, "name");
    const path = requireString(item.path, `${packageName} path`);
    if (!isCanonicalPackagePath(path)) {
      throw new Error(`Worker OCI package authority path ${path} is invalid.`);
    }
    if (authority.has(path)) {
      throw new Error(`Worker OCI package authority has duplicate path ${path}.`);
    }
    const supplementalLicenseTextIds = parseStringArray(
      item.supplementalLicenseTextIds,
      `${path} supplementalLicenseTextIds`,
    );
    for (const id of supplementalLicenseTextIds) {
      if (!supplementalLicenseTexts.has(id)) {
        throw new Error(`${path} references unknown supplemental license text ${id}.`);
      }
    }
    const bundledLicenseFiles = parseBundledLicenseFiles(item.bundledLicenseFiles, path);
    const packageFiles = parsePackageFiles(item.packageFiles, path);
    const entry: WorkerOciPackageAuthorityEntry = {
      package: packageName,
      path,
      version: requireString(item.version, `${path} version`),
      sourceUrl: requireString(item.sourceUrl, `${path} sourceUrl`),
      integrity: requireString(item.integrity, `${path} integrity`),
      license: requireString(item.license, `${path} license`),
      supplementalLicenseTextIds,
      bundledLicenseFiles,
      packageFiles,
    };
    validateAuthorityAgainstLock(entry, lock);
    const packagePathName = packageNameFromPath(path);
    const direct =
      path === `node_modules/${packagePathName}` && packagePathName in lock.rootDependencies;
    if (!direct && !hasPackageBindingReferences(entry)) {
      throw new Error(
        `${path} package authority is not a direct package.json dependency and has no supplemental or bundled license reference.`,
      );
    }
    authority.set(path, entry);
  }
  const referencedIds = new Set(
    [...authority.values()].flatMap((item) => item.supplementalLicenseTextIds),
  );
  for (const id of supplementalLicenseTexts.keys()) {
    if (!referencedIds.has(id)) {
      throw new Error(`Worker OCI supplemental license text ${id} is unreferenced.`);
    }
  }
  return { registry, blockedReasons, packages: authority, supplementalLicenseTexts };
}

function validateAuthorityAgainstLock(entry: WorkerOciPackageAuthorityEntry, lock: NpmLock): void {
  const lockEntry = lock.packages[entry.path];
  if (!lockEntry) {
    throw new Error(`${entry.path} package authority has no exact package-lock.json record.`);
  }
  const pathName = packageNameFromPath(entry.path);
  const direct = entry.path === `node_modules/${pathName}` && pathName in lock.rootDependencies;
  const lockName = typeof lockEntry.name === "string" ? lockEntry.name : pathName;
  // Direct records without license references retain the historical
  // NOASSERTION/blocked behavior. A direct record with a reference is a
  // license binding and must be exact, just like every transitive record.
  if (direct && !hasPackageBindingReferences(entry)) return;
  if (entry.package !== lockName) {
    throw new Error(
      `${entry.path} package authority name ${entry.package} does not match package-lock.json name ${lockName}.`,
    );
  }
  if (entry.version !== lockEntry.version) {
    throw new Error(`${entry.path} package authority version does not match package-lock.json.`);
  }
  if (entry.sourceUrl !== lockEntry.resolved) {
    throw new Error(`${entry.path} package authority sourceUrl does not match package-lock.json.`);
  }
  if (entry.integrity !== lockEntry.integrity || !isCanonicalSha512Sri(entry.integrity)) {
    throw new Error(`${entry.path} package authority integrity does not match package-lock.json.`);
  }
  if (entry.license !== lockEntry.license) {
    throw new Error(`${entry.path} package authority license does not match package-lock.json.`);
  }
}

function parseSupplementalSource(value: unknown, id: string): WorkerOciSupplementalSource {
  if (!isRecord(value)) {
    throw new Error(`Worker OCI supplemental license text ${id} source is invalid.`);
  }
  const kind = requireString(value.kind, `${id} source kind`);
  if (kind === "repository") {
    const repository = requireHttpsUrl(value.repository, `${id} repository`);
    const commit = requireString(value.commit, `${id} commit`);
    const path = requireSafeRelative(value.path, `${id} source path`);
    if (!/^[0-9a-f]{40}$/iu.test(commit)) {
      throw new Error(`Worker OCI supplemental license text ${id} commit is invalid.`);
    }
    return { kind, repository, commit, path };
  }
  if (kind === "npm-tarball") {
    const url = requireHttpsUrl(value.url, `${id} URL`);
    const integrity = requireString(value.integrity, `${id} source integrity`);
    if (!isCanonicalSha512Sri(integrity)) {
      throw new Error(`Worker OCI supplemental license text ${id} source integrity is invalid.`);
    }
    return {
      kind,
      url,
      integrity,
      member: requireSafeRelative(value.member, `${id} tarball member`),
    };
  }
  if (kind === "source-archive") {
    return {
      kind,
      url: requireHttpsUrl(value.url, `${id} URL`),
      sha256: requireSha256(value.sha256, `${id} source archive sha256`),
      member: requireSafeRelative(value.member, `${id} source path`),
    };
  }
  throw new Error(`Worker OCI supplemental license text ${id} source kind ${kind} is invalid.`);
}

function parseBundledLicenseFiles(
  value: unknown,
  packagePath: string,
): ReadonlyArray<WorkerOciBundledLicenseFile> {
  if (value === undefined) return [];
  if (!Array.isArray(value)) {
    throw new Error(`${packagePath} bundledLicenseFiles is invalid.`);
  }
  const paths = new Set<string>();
  return value
    .map((item) => {
      if (!isRecord(item)) throw new Error(`${packagePath} bundled license file is invalid.`);
      const path = requireSafeRelative(item.path, `${packagePath} bundled license path`);
      if (paths.has(path)) {
        throw new Error(`${packagePath} has duplicate bundled license path ${path}.`);
      }
      paths.add(path);
      return { path, sha256: requireSha256(item.sha256, `${packagePath} ${path} sha256`) };
    })
    .toSorted((left, right) => left.path.localeCompare(right.path));
}

function parsePackageFiles(
  value: unknown,
  packagePath: string,
): ReadonlyArray<WorkerOciPackageFile> | undefined {
  if (value === undefined) return undefined;
  if (!Array.isArray(value) || value.length === 0) {
    throw new Error(`${packagePath} packageFiles must be a non-empty array.`);
  }
  const paths = new Set<string>();
  const result = value.map((item) => {
    if (!isRecord(item)) throw new Error(`${packagePath} package file is invalid.`);
    const path = requireSafeRelative(item.path, `${packagePath} package file path`);
    if (paths.has(path)) {
      throw new Error(`${packagePath} has duplicate package file path ${path}.`);
    }
    if (
      [...paths].some(
        (existing) => path.startsWith(`${existing}/`) || existing.startsWith(`${path}/`),
      )
    ) {
      throw new Error(
        `${packagePath} package file paths have an ancestry collision involving ${path}.`,
      );
    }
    paths.add(path);
    const sha256Value = requireSha256(item.sha256, `${packagePath} ${path} sha256`);
    const size = item.size;
    if (
      typeof size !== "number" ||
      !Number.isSafeInteger(size) ||
      size < 0 ||
      size > MAX_PACKAGE_FILE_BYTES
    ) {
      throw new Error(
        `${packagePath} package file ${path} size must be an integer from 0 through ${MAX_PACKAGE_FILE_BYTES}.`,
      );
    }
    return { path, sha256: sha256Value, size };
  });
  if (!paths.has("package.json")) {
    throw new Error(`${packagePath} packageFiles must contain package.json.`);
  }
  return result.toSorted((left, right) => left.path.localeCompare(right.path));
}

function readRegularSource(root: string, sourcePath: string): Buffer {
  const absolute = join(root, sourcePath);
  const relativePath = relative(root, absolute);
  if (relativePath.startsWith("..") || relativePath.includes("\0")) {
    throw new Error(`Worker OCI source path ${sourcePath} escapes repository root.`);
  }
  let current = root;
  for (const component of relativePath.split("/").filter(Boolean)) {
    current = join(current, component);
    const stat = lstatSync(current);
    if (stat.isSymbolicLink()) {
      throw new Error(`Worker OCI source path ${sourcePath} contains a symlink.`);
    }
  }
  const stat = lstatSync(absolute);
  if (!stat.isFile())
    throw new Error(`Worker OCI source path ${sourcePath} is not a regular file.`);
  return readFileSync(absolute);
}

function architecturesFor(
  entry: NpmPackageRecord | undefined,
): ReadonlyArray<"linux/amd64" | "linux/arm64"> {
  if (!Array.isArray(entry?.cpu)) return ["linux/amd64", "linux/arm64"];
  const architectures: Array<"linux/amd64" | "linux/arm64"> = [];
  if (entry.cpu.includes("x64")) architectures.push("linux/amd64");
  if (entry.cpu.includes("arm64")) architectures.push("linux/arm64");
  return architectures.length > 0 ? architectures : ["linux/amd64", "linux/arm64"];
}

function isCanonicalSha512Sri(value: string): boolean {
  const match = /^sha512-(?<digest>[A-Za-z0-9+/]+={0,2})$/u.exec(value);
  if (!match?.groups?.digest) return false;
  const decoded = Buffer.from(match.groups.digest, "base64");
  return decoded.length === 64 && decoded.toString("base64") === match.groups.digest;
}

function parseBaseImage(dockerfile: string): {
  readonly reference: string;
  readonly digest: string | typeof NOASSERTION;
} {
  const match = /^ARG BASE_IMAGE=(\S+)$/mu.exec(dockerfile);
  if (!match?.[1]) throw new Error("Worker Dockerfile base image is missing.");
  const reference = match[1];
  const digestMatch = /@(?<digest>sha256:[0-9a-f]{64})$/u.exec(reference);
  return { reference, digest: digestMatch?.groups?.digest ?? NOASSERTION };
}

function parseRegistry(dockerfile: string): string | typeof NOASSERTION {
  const match = /^ENV NPM_CONFIG_REGISTRY=(\S+)$/mu.exec(dockerfile);
  return match?.[1]?.replace(/^['"]|['"]$/gu, "") ?? NOASSERTION;
}

function parseOsInstall(
  dockerfile: string,
): ReadonlyArray<Readonly<{ readonly package: string; readonly license: typeof NOASSERTION }>> {
  const match = /apt-get install[\s\S]*?--yes\s+([^\\\n]+)/u.exec(dockerfile);
  if (!match?.[1]) return [];
  return match[1]
    .trim()
    .split(/\s+/u)
    .filter(Boolean)
    .map((packageName) => ({ package: packageName, license: NOASSERTION }));
}

function renderNotice(manifest: WorkerOciInstallManifest): string {
  const lines = [
    "# Cloud Agents Worker OCI third-party notice boundary",
    "",
    `Status: ${manifest.status}`,
    "",
    "Direct package bytes come from the dedicated npm lock. A direct license is reported only when the versioned Worker OCI package authority exactly agrees with the lock record; no transitive license is inferred from package names or registry metadata.",
    "",
    `Dockerfile: \`${manifest.source.dockerfile}\` (SHA-256 \`${manifest.source.dockerfileSha256}\`)`,
    `Package manifest: \`${manifest.source.packageManifest}\` (SHA-256 \`${manifest.source.packageManifestSha256}\`)`,
    `Lockfile: \`${manifest.source.lockfile}\` (SHA-256 \`${manifest.source.lockfileSha256}\`)`,
    `Package authority: \`${manifest.source.packageAuthority}\` (SHA-256 \`${manifest.source.packageAuthoritySha256}\`)`,
    `Installed inventory collector: \`${manifest.source.installedInventoryCollector}\` (SHA-256 \`${manifest.source.installedInventoryCollectorSha256}\`)`,
    `NPM registry: \`${manifest.registry}\``,
    "",
    "## Direct npm installs",
    "",
    "| Package | Version | Architectures | Source | Integrity | License |",
    "| --- | --- | --- | --- | --- | --- |",
    ...manifest.directInstalls.map(
      (item) =>
        `| \`${item.package}\` | \`${item.version}\` | ${item.architectures.join(", ")} | ${item.sourceUrl} | ${item.integrity} | ${item.license} |`,
    ),
    "",
    "## Supplemental license text provenance",
    "",
    "These supplemental artifacts reproduce source-provenance material and are bound by their captured bytes. Their presence, source metadata, and digest do not constitute legal approval or replace the applicable license terms.",
    "",
    "| ID | Image path | SHA-256 | Source |",
    "| --- | --- | --- | --- |",
    ...manifest.supplementalLicenseTexts.map(
      (item) =>
        `| \`${item.id}\` | \`${item.imagePath}\` | \`${item.sha256}\` | ${renderSupplementalSource(item.source)} |`,
    ),
    "",
    "## Package bindings",
    "",
    "| Lock path | Package | Version | Integrity | Supplemental text IDs | Bundled license files |",
    "| --- | --- | --- | --- | --- | --- |",
    ...manifest.packageBindings.map(
      (item) =>
        `| \`${item.path}\` | \`${item.name}\` | \`${item.version}\` | \`${item.integrity}\` | ${item.supplementalLicenseTextIds.map((id) => `\`${id}\``).join(", ") || "—"} | ${item.bundledLicenseFiles.map((file) => `\`${file.path}\` (${file.sha256})`).join(", ") || "—"} |`,
    ),
    "",
    "## Bound package member files",
    "",
    "These entries bind selected regular-file bytes from lock-integrity-verified npm tarballs to the installed package directory. They are package-content evidence only; they do not assert license meaning or native binary build provenance.",
    "",
    "| Lock path | Package member | Size | SHA-256 |",
    "| --- | --- | ---: | --- |",
    ...manifest.packageBindings.flatMap(
      (item) =>
        item.packageFiles?.map(
          (file) => `| \`${item.path}\` | \`${file.path}\` | ${file.size} | \`${file.sha256}\` |`,
        ) ?? [],
    ),
    "",
    "## Base image and OS packages",
    "",
    `- Base image: \`${manifest.baseImage.reference}\`; digest: \`${manifest.baseImage.digest}\`; license: \`${manifest.baseImage.license}\`.`,
    ...manifest.osInstall.map(
      (item) => `- apt package \`${item.package}\`; license: \`${item.license}\`.`,
    ),
    "",
    "## Boundaries",
    "",
    "The npm lock binds registry tarball bytes but does not prove the installed platform-specific transitive closure or its license inventory. Bound package member files are a narrow tarball-to-installed-byte check and do not prove native source-build provenance. The Debian/base-image closure, vulnerability state, signatures, and provenance are also not proven by this manifest.",
    ...manifest.blockedReasons.map((reason) => `- BLOCKED: ${reason}`),
    "",
  ];
  return `${lines.join("\n")}\n`;
}

function renderSupplementalSource(source: WorkerOciSupplementalSource): string {
  if (source.kind === "repository") {
    return `repository ${source.repository}@${source.commit}:${source.path}`;
  }
  if (source.kind === "npm-tarball") {
    return `npm tarball ${source.url}#${source.member} (${source.integrity})`;
  }
  return `source archive ${source.url}#${source.member} (SHA-256 ${source.sha256})`;
}

function sha256(value: string | Buffer): string {
  return createHash("sha256").update(value).digest("hex");
}

function recordsEqual(left: Record<string, string>, right: Record<string, string>): boolean {
  return (
    JSON.stringify(Object.entries(left).toSorted()) ===
    JSON.stringify(Object.entries(right).toSorted())
  );
}

function hasPackageBindingReferences(entry: WorkerOciPackageAuthorityEntry): boolean {
  return (
    entry.supplementalLicenseTextIds.length > 0 ||
    entry.bundledLicenseFiles.length > 0 ||
    (entry.packageFiles?.length ?? 0) > 0
  );
}

function packageNameFromPath(path: string): string {
  const components = path.split("/");
  const nodeModulesIndex = components.lastIndexOf("node_modules");
  if (nodeModulesIndex < 0 || nodeModulesIndex === components.length - 1) return "";
  const name = components[nodeModulesIndex + 1];
  return name.startsWith("@") ? `${name}/${components[nodeModulesIndex + 2] ?? ""}` : name;
}

function isCanonicalPackagePath(path: string): boolean {
  return (
    path.startsWith("node_modules/") &&
    isSafeRelative(path) &&
    path.endsWith(`/${packageNameFromPath(path)}`) &&
    !path.includes("//")
  );
}

function isSafeArtifactPath(path: string): boolean {
  return (
    path.startsWith(SUPPLEMENTAL_ARTIFACT_PREFIX) &&
    isSafeRelative(path.slice(SUPPLEMENTAL_ARTIFACT_PREFIX.length))
  );
}

function isSafeRelative(value: string): boolean {
  return (
    value.length > 0 &&
    !value.startsWith("/") &&
    !value.startsWith("\\") &&
    !value.includes("\\") &&
    !value.includes("\0") &&
    !/[\u0000-\u001f\u007f]/u.test(value) &&
    value.split("/").every((part) => part.length > 0 && part !== "." && part !== "..")
  );
}

function parseStringArray(value: unknown, field: string): ReadonlyArray<string> {
  if (value === undefined) return [];
  return requireStringArray(value, field);
}

function requireStringArray(value: unknown, field: string): ReadonlyArray<string> {
  if (
    !Array.isArray(value) ||
    value.some((item) => typeof item !== "string" || item.length === 0)
  ) {
    throw new Error(`Worker OCI package authority ${field} is invalid.`);
  }
  const result = value as string[];
  if (new Set(result).size !== result.length) {
    throw new Error(`Worker OCI package authority ${field} contains duplicates.`);
  }
  return [...result];
}

function requireHttpsUrl(value: unknown, field: string): string {
  const url = requireString(value, field);
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "https:" || parsed.username || parsed.password || parsed.hash) {
      throw new Error();
    }
  } catch {
    throw new Error(`Worker OCI package authority ${field} must be an HTTPS URL.`);
  }
  return url;
}

function requireSafeRelative(value: unknown, field: string): string {
  const path = requireString(value, field);
  if (!isSafeRelative(path)) throw new Error(`Worker OCI package authority ${field} is invalid.`);
  return path;
}

function requireSha256(value: unknown, field: string): string {
  const digest = requireString(value, field);
  if (!/^[0-9a-f]{64}$/u.test(digest)) {
    throw new Error(`Worker OCI package authority ${field} must be a lowercase SHA-256 digest.`);
  }
  return digest;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function requireString(value: unknown, field: string): string {
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`Worker OCI package authority ${field} is invalid.`);
  }
  return value;
}

function requireStringRecord(value: unknown, field: string): Record<string, string> {
  if (value === undefined) return {};
  if (!isRecord(value) || Object.values(value).some((item) => typeof item !== "string")) {
    throw new Error(`Worker tool ${field} is invalid.`);
  }
  return value as Record<string, string>;
}
