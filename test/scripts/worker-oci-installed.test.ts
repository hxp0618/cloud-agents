import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { describe, expect, it, onTestFinished } from "vitest";

import { captureWorkerOciInstalledInventory } from "../../scripts/lib/worker-oci-installed";

const COLLECTOR = "scripts/lib/worker-oci-installed.ts";
const TOOLS = "opt/cloud-agents-worker-tools";
const ROOT_LOCK = `${TOOLS}/package-lock.json`;
const HIDDEN_LOCK = `${TOOLS}/node_modules/.package-lock.json`;
const STATIC_MANIFEST = "usr/share/doc/cloud-agents/worker-oci-install-manifest.json";

describe("Worker OCI installed inventory", () => {
  it("captures a deterministic alias-aware npm and Debian inventory", () => {
    const root = createFixture("deterministic");

    const first = capture(root);
    const second = capture(root);

    expect(first).toEqual(second);
    expect(first).toMatchObject({
      schemaVersion: 1,
      kind: "cloud-agents-worker-oci-installed-inventory",
      platform: "linux/arm64",
      status: "BLOCKED",
      packageRecordConsistency: "PASS",
      licenseArtifactPresence: "PASS",
      npm: {
        status: "PASS",
        packages: [
          {
            physicalPath: "node_modules/alias-package",
            name: "actual-package",
            version: "1.0.0",
            declaredLicense: "SEE LICENSE IN LICENSE.txt",
            licenseArtifactPresence: "PASS",
            packageContentVerification: "NOT_RUN",
            packageFiles: null,
            licenseFiles: [
              expect.objectContaining({
                path: "LICENSE.txt",
                sha256: expect.stringMatching(/^[0-9a-f]{64}$/u),
                size: 13,
              }),
            ],
          },
        ],
      },
      debian: {
        status: "PASS",
        packages: [
          expect.objectContaining({
            package: "fixture-package",
            version: "1.2.3-1",
            architecture: "arm64",
            copyright: expect.objectContaining({
              path: "/usr/share/doc/fixture-package/copyright",
            }),
          }),
        ],
        commonLicenses: [expect.objectContaining({ path: "/usr/share/common-licenses/GPL-3" })],
      },
      fullSupplyChain: { status: "BLOCKED" },
    });
    expect(first.source.packageManifestSha256).toMatch(/^[0-9a-f]{64}$/u);
    expect(first.source.staticManifestSha256).toMatch(/^[0-9a-f]{64}$/u);
    expect(first.source.installedInventoryCollectorSha256).toBe(sha256(readFileSync(COLLECTOR)));

    writeFileSync(join(root, TOOLS, "node_modules/alias-package/LICENSE.txt"), "changed text\n");
    const changed = capture(root);
    expect(changed.npm.packages[0]!.licenseFiles[0]!.sha256).not.toBe(
      first.npm.packages[0]!.licenseFiles[0]!.sha256,
    );
  });

  it("reports missing npm license text as coverage BLOCKED without hiding the inventory", () => {
    const root = createFixture("npm-license-missing");

    rmSync(join(root, TOOLS, "node_modules/alias-package/LICENSE.txt"));
    const result = capture(root);
    expect(result.licenseArtifactPresence).toBe("BLOCKED");
    expect(result.npm.status).toBe("BLOCKED");
    expect(result.npm.packages[0]).toMatchObject({
      licenseArtifactPresence: "BLOCKED",
      licenseFiles: [],
    });
    expect(result.blockedReasons).toContain(
      "npm node_modules/alias-package declares SEE LICENSE IN LICENSE.txt but that file is missing.",
    );
  });

  it("rejects missing and extra physical npm package roots", () => {
    const missing = createFixture("missing-package");
    const extra = createFixture("extra-package");

    rmSync(join(missing, TOOLS, "node_modules/alias-package"), {
      recursive: true,
      force: true,
    });
    expect(() => capture(missing)).toThrow(/installed npm package set.*missing/u);

    writeJson(join(extra, TOOLS, "node_modules/extra/package.json"), {
      name: "extra",
      version: "1.0.0",
      license: "MIT",
    });
    expect(() => capture(extra)).toThrow(/installed npm package set.*extra/u);
  });

  it("rejects hidden-lock drift and static root-lock byte drift", () => {
    const hidden = createFixture("hidden-lock-drift");
    const rootLock = createFixture("root-lock-drift");

    const hiddenPath = join(hidden, HIDDEN_LOCK);
    const hiddenJson = readJson(hiddenPath);
    hiddenJson.packages["node_modules/alias-package"].integrity = "sha512-drift";
    writeJson(hiddenPath, hiddenJson);
    expect(() => capture(hidden)).toThrow(/hidden lock.*integrity/u);

    writeFileSync(join(rootLock, ROOT_LOCK), "{}\n");
    expect(() => capture(rootLock)).toThrow(/static manifest.*package-lock/u);
  });

  it("rejects protected input symlinks", () => {
    const root = createFixture("protected-input-symlink");

    const lockPath = join(root, ROOT_LOCK);
    const movedLock = join(root, TOOLS, "moved-package-lock.json");
    writeFileSync(movedLock, readFileSync(lockPath));
    rmSync(lockPath);
    symlinkSync("moved-package-lock.json", lockPath);
    rewriteStaticManifest(root);
    expect(() => capture(root)).toThrow(/package-lock.json.*symbolic link/u);
  });

  it("requires every non-optional direct dependency to be physically installed", () => {
    const root = createFixture("direct-dependency-missing");

    const packagePath = join(root, TOOLS, "package.json");
    const packageManifest = readJson(packagePath);
    packageManifest.dependencies.missing = "1.0.0";
    writeJson(packagePath, packageManifest);
    rewriteStaticManifest(root);
    expect(() => capture(root)).toThrow(/required direct dependency missing is not installed/u);
  });

  it("rejects runtime, package metadata, and Debian architecture mismatches", () => {
    const runtime = createFixture("runtime-arch");
    const npm = createFixture("npm-arch");
    const debian = createFixture("debian-arch");

    expect(() =>
      captureWorkerOciInstalledInventory(runtime, "linux/arm64", {
        platform: "linux",
        arch: "x64",
      }),
    ).toThrow(/runtime architecture/u);

    const lockPath = join(npm, ROOT_LOCK);
    const lock = readJson(lockPath);
    lock.packages["node_modules/alias-package"].cpu = ["x64"];
    writeJson(lockPath, lock);
    rewriteStaticManifest(npm);
    expect(() => capture(npm)).toThrow(/does not allow cpu arm64/u);

    const statusPath = join(debian, "var/lib/dpkg/status");
    writeFileSync(statusPath, dpkgStatus("amd64"));
    expect(() => capture(debian)).toThrow(/Debian package.*architecture amd64/u);
  });

  it("rejects package-root symlinks even when they stay inside the tools directory", () => {
    const root = createFixture("package-symlink");

    const packagePath = join(root, TOOLS, "node_modules/alias-package");
    const targetPath = join(root, TOOLS, "alias-target");
    mkdirSync(targetPath, { recursive: true });
    writeJson(join(targetPath, "package.json"), {
      name: "actual-package",
      version: "1.0.0",
      license: "SEE LICENSE IN LICENSE.txt",
    });
    rmSync(packagePath, { recursive: true, force: true });
    symlinkSync("../alias-target", packagePath);
    expect(() => capture(root)).toThrow(/package root.*symbolic link/u);
  });

  it("rejects package metadata and nested node_modules symlinks", () => {
    const metadata = createFixture("package-json-symlink");
    const nested = createFixture("nested-node-modules-symlink");
    const license = createFixture("license-symlink-escape");

    const packageJson = join(metadata, TOOLS, "node_modules/alias-package/package.json");
    const externalJson = join(metadata, TOOLS, "outside-package.json");
    writeJson(externalJson, {
      name: "actual-package",
      version: "1.0.0",
      license: "SEE LICENSE IN LICENSE.txt",
    });
    rmSync(packageJson);
    symlinkSync("../../outside-package.json", packageJson);
    expect(() => capture(metadata)).toThrow(/package.json.*symbolic link/u);

    const nestedModules = join(nested, TOOLS, "node_modules/alias-package/node_modules");
    symlinkSync("../../..", nestedModules);
    expect(() => capture(nested)).toThrow(/node_modules.*symbolic link/u);

    const licensePath = join(license, TOOLS, "node_modules/alias-package/LICENSE.txt");
    writeFile(join(license, TOOLS, "outside-license.txt"), "outside\n");
    rmSync(licensePath);
    symlinkSync("../../outside-license.txt", licensePath);
    expect(() => capture(license)).toThrow(/resolves outside its allowed license roots/u);
  });

  it("rejects an empty npm inventory and unsafe or duplicate Debian identities", () => {
    const empty = createFixture("empty-npm");
    const unsafe = createFixture("unsafe-debian-name");
    const duplicate = createFixture("duplicate-debian-record");

    rmSync(join(empty, TOOLS, "node_modules/alias-package"), {
      recursive: true,
      force: true,
    });
    writeJson(join(empty, ROOT_LOCK), {
      name: "fixture-worker-tools",
      lockfileVersion: 3,
      packages: { "": {} },
    });
    writeJson(join(empty, HIDDEN_LOCK), {
      name: "fixture-worker-tools",
      lockfileVersion: 3,
      packages: {},
    });
    rewriteStaticManifest(empty);
    expect(() => capture(empty)).toThrow(/npm inventory is empty/u);

    writeFileSync(
      join(unsafe, "var/lib/dpkg/status"),
      dpkgStatus("arm64").replace("fixture-package", "../escape"),
    );
    expect(() => capture(unsafe)).toThrow(/invalid Debian package name/u);

    const status = dpkgStatus("arm64");
    writeFileSync(join(duplicate, "var/lib/dpkg/status"), `${status}\n${status}`);
    expect(() => capture(duplicate)).toThrow(/duplicate installed Debian package/u);
  });

  it("reports a missing Debian copyright file as coverage BLOCKED", () => {
    const root = createFixture("debian-license-missing");

    rmSync(join(root, "usr/share/doc/fixture-package/copyright"));
    const result = capture(root);
    expect(result.debian.status).toBe("BLOCKED");
    expect(result.licenseArtifactPresence).toBe("BLOCKED");
    expect(result.blockedReasons).toContain(
      "Debian package fixture-package has no /usr/share/doc/fixture-package/copyright file.",
    );
  });

  it("reports NOTICE-only packages as filename-based artifact presence", () => {
    const root = createFixture("notice-only");

    const packageRoot = join(root, TOOLS, "node_modules/alias-package");
    rmSync(join(packageRoot, "LICENSE.txt"));
    writeFile(join(packageRoot, "NOTICE"), "notice only\n");
    const packagePath = join(packageRoot, "package.json");
    const packageJson = readJson(packagePath);
    packageJson.license = "MIT";
    writeJson(packagePath, packageJson);

    const result = capture(root);
    expect(result.npm.packages[0]).toMatchObject({
      licenseArtifactPresence: "PASS",
      licenseFiles: [expect.objectContaining({ path: "NOTICE" })],
    });
    expect(result.fullSupplyChain.limits).toContain(
      "License artifact presence is filename-based and does not prove license meaning, completeness, or legal approval.",
    );
  });

  it("binds an explicit bundled README and keeps supplemental text separate", () => {
    const root = createFixture("license-bindings");

    const packageRoot = join(root, TOOLS, "node_modules/alias-package");
    rmSync(join(packageRoot, "LICENSE.txt"));
    const bundledReadme = "Copyright (c) fixture\nMIT License\n";
    writeFile(join(packageRoot, "README.md"), bundledReadme);
    const packageJson = readJson(join(packageRoot, "package.json"));
    packageJson.license = "MIT";
    writeJson(join(packageRoot, "package.json"), packageJson);
    const supplemental = "Supplemental license text\n";
    writeFile(
      join(root, "usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/fixture.txt"),
      supplemental,
    );
    writeStaticAuthority(root, {
      supplementalLicenseTexts: [supplementalAuthority(root, supplemental)],
      packageBindings: [
        licenseBinding(root, {
          bundledLicenseFiles: [{ path: "README.md", sha256: sha256(Buffer.from(bundledReadme)) }],
          supplementalLicenseTextIds: ["fixture-supplemental"],
        }),
      ],
    });

    const result = capture(root);
    expect(result.npm).toMatchObject({ status: "PASS" });
    expect(result.npm.packages[0]).toMatchObject({
      licenseArtifactPresence: "PASS",
      licenseFiles: [expect.objectContaining({ path: "README.md" })],
    });
    expect(result.supplementalLicenseTexts).toEqual([
      expect.objectContaining({
        id: "fixture-supplemental",
        path: "/usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/fixture.txt",
        sha256: sha256(Buffer.from(supplemental)),
        packagePaths: ["node_modules/alias-package"],
      }),
    ]);
    expect(result.npm.packages[0]!.licenseFiles).not.toContainEqual(
      expect.objectContaining({
        path: "/usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/fixture.txt",
      }),
    );
    expect(result.fullSupplyChain.status).toBe("BLOCKED");
  });

  it("does not promote a supplemental-only binding to npm license coverage", () => {
    const root = createFixture("supplemental-only");

    const packageRoot = join(root, TOOLS, "node_modules/alias-package");
    rmSync(join(packageRoot, "LICENSE.txt"));
    const supplemental = "Supplemental only\n";
    writeFile(
      join(root, "usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/only.txt"),
      supplemental,
    );
    writeStaticAuthority(root, {
      supplementalLicenseTexts: [
        supplementalAuthority(root, supplemental, {
          id: "supplemental-only",
          suffix: "only.txt",
        }),
      ],
      packageBindings: [
        licenseBinding(root, { supplementalLicenseTextIds: ["supplemental-only"] }),
      ],
    });

    const result = capture(root);
    expect(result.npm.status).toBe("BLOCKED");
    expect(result.npm.packages[0]).toMatchObject({
      licenseArtifactPresence: "BLOCKED",
      licenseFiles: [],
    });
    expect(result.supplementalLicenseTexts[0]!.packagePaths).toEqual([
      "node_modules/alias-package",
    ]);
  });

  it("accepts a canonical npm tarball source and rejects invalid SRI", () => {
    const valid = createFixture("npm-tarball-source-valid");
    const invalid = createFixture("npm-tarball-source-invalid");

    const content = "tarball-sourced license\n";
    for (const root of [valid, invalid]) {
      writeFile(
        join(root, "usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/tarball.txt"),
        content,
      );
    }
    writeStaticAuthority(valid, {
      supplementalLicenseTexts: [
        {
          ...supplementalAuthority(valid, content, {
            id: "tarball-source",
            suffix: "tarball.txt",
          }),
          source: {
            kind: "npm-tarball",
            url: "https://registry.example/fixture.tgz",
            integrity: `sha512-${Buffer.alloc(64, 3).toString("base64")}`,
            member: "package/LICENSE",
          },
        },
      ],
      packageBindings: [licenseBinding(valid, { supplementalLicenseTextIds: ["tarball-source"] })],
    });
    const result = capture(valid);
    expect(result.supplementalLicenseTexts[0]!.source).toMatchObject({
      kind: "npm-tarball",
      member: "package/LICENSE",
    });

    writeStaticAuthority(invalid, {
      supplementalLicenseTexts: [
        {
          ...supplementalAuthority(invalid, content, {
            id: "tarball-source",
            suffix: "tarball.txt",
          }),
          source: {
            kind: "npm-tarball",
            url: "https://registry.example/fixture.tgz",
            integrity: "sha512-AAAA",
            member: "package/LICENSE",
          },
        },
      ],
      packageBindings: [
        licenseBinding(invalid, { supplementalLicenseTextIds: ["tarball-source"] }),
      ],
    });
    expect(() => capture(invalid)).toThrow(/canonical SHA-512 SRI/u);
  });

  it("accepts fixed source-archive provenance and rejects credentials, traversal, and bad digests", () => {
    const valid = createFixture("source-archive-source-valid");

    const content = "source archive license\n";
    writeFile(
      join(valid, "usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/archive.txt"),
      content,
    );
    writeStaticAuthority(valid, {
      supplementalLicenseTexts: [
        {
          ...supplementalAuthority(valid, content, {
            id: "source-archive",
            suffix: "archive.txt",
          }),
          source: {
            kind: "source-archive",
            url: "https://download.gnome.org/sources/glib/2.90/glib-2.90.0.tar.xz",
            sha256: "a".repeat(64),
            member: "glib-2.90.0/COPYING",
          },
        },
      ],
      packageBindings: [licenseBinding(valid, { supplementalLicenseTextIds: ["source-archive"] })],
    });
    const result = capture(valid);
    expect(result.supplementalLicenseTexts[0]?.source).toEqual({
      kind: "source-archive",
      url: "https://download.gnome.org/sources/glib/2.90/glib-2.90.0.tar.xz",
      sha256: "a".repeat(64),
      member: "glib-2.90.0/COPYING",
    });

    const cases: Array<[string, JsonRecord, RegExp]> = [
      [
        "source-archive-credentials",
        {
          kind: "source-archive",
          url: "https://user:password@download.gnome.org/sources/glib/2.90/glib-2.90.0.tar.xz",
          sha256: "a".repeat(64),
          member: "glib-2.90.0/COPYING",
        },
        /HTTPS URL/u,
      ],
      [
        "source-archive-traversal",
        {
          kind: "source-archive",
          url: "https://download.gnome.org/sources/glib/2.90/glib-2.90.0.tar.xz",
          sha256: "a".repeat(64),
          member: "../COPYING",
        },
        /safe relative path/u,
      ],
      [
        "source-archive-digest",
        {
          kind: "source-archive",
          url: "https://download.gnome.org/sources/glib/2.90/glib-2.90.0.tar.xz",
          sha256: "sha256-not-hex",
          member: "glib-2.90.0/COPYING",
        },
        /lowercase SHA-256 digest/u,
      ],
    ];
    for (const [name, member] of [
      ["nul", "source/LICENSE\0"],
      ["newline", "source/LI\nCENSE"],
      ["delete", "source/LI\u007fCENSE"],
    ]) {
      cases.push([
        `source-archive-${name}`,
        {
          kind: "source-archive",
          url: "https://example.com/source.tar.xz",
          sha256: "a".repeat(64),
          member,
        },
        /safe relative path/u,
      ]);
    }
    for (const [name, source, expected] of cases) {
      const root = createFixture(name);

      const content = "source archive license\n";
      writeFile(
        join(root, "usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/archive.txt"),
        content,
      );
      writeStaticAuthority(root, {
        supplementalLicenseTexts: [
          {
            ...supplementalAuthority(root, content, {
              id: "source-archive",
              suffix: "archive.txt",
            }),
            source,
          },
        ],
        packageBindings: [licenseBinding(root, { supplementalLicenseTextIds: ["source-archive"] })],
      });
      expect(() => capture(root)).toThrow(expected);
    }
  });

  it("validates opposite-architecture bindings against the root lock but filters them", () => {
    const root = createFixture("opposite-architecture-binding");

    const oppositePath = "node_modules/opposite-package";
    const oppositeRecord = {
      name: "opposite-package",
      version: "2.0.0",
      resolved: "https://registry.example/opposite-package.tgz",
      integrity: `sha512-${Buffer.alloc(64, 9).toString("base64")}`,
      license: "MIT",
      os: ["linux"],
      cpu: ["x64"],
    };
    const rootLock = readJson(join(root, ROOT_LOCK));
    rootLock.packages[oppositePath] = oppositeRecord;
    writeJson(join(root, ROOT_LOCK), rootLock);
    rewriteStaticManifest(root);
    writeStaticAuthority(root, {
      supplementalLicenseTexts: [],
      packageBindings: [
        licenseBinding(root, {
          path: oppositePath,
          name: "opposite-package",
          version: "2.0.0",
          integrity: oppositeRecord.integrity,
          packageFiles: [
            {
              path: "package.json",
              sha256: "0".repeat(64),
              size: 1,
            },
          ],
        }),
      ],
    });

    const result = capture(root);
    expect(result.npm.packages.map(({ physicalPath }) => physicalPath)).toEqual([
      "node_modules/alias-package",
    ]);
    expect(result.supplementalLicenseTexts).toEqual([]);
  });

  it("verifies every file for a bound package and keeps content status separate", () => {
    const root = createFixture("package-files-pass");

    const packageRoot = join(root, TOOLS, "node_modules/alias-package");
    writeFile(join(packageRoot, ".hidden"), "hidden\n");
    mkdirSync(join(packageRoot, "bin"), { recursive: true });
    writeFileSync(join(packageRoot, "bin/tool"), Buffer.alloc(2 * 1024 * 1024, 7));
    const nestedPath = "node_modules/alias-package/node_modules/nested";
    const nestedRecord = {
      name: "nested",
      version: "1.0.0",
      resolved: "https://registry.example/nested.tgz",
      integrity: `sha512-${Buffer.alloc(64, 8).toString("base64")}`,
      license: "MIT",
      os: ["linux"],
      cpu: ["arm64"],
    };
    const rootLock = readJson(join(root, ROOT_LOCK));
    const hiddenLock = readJson(join(root, HIDDEN_LOCK));
    rootLock.packages[nestedPath] = nestedRecord;
    hiddenLock.packages[nestedPath] = nestedRecord;
    writeJson(join(root, ROOT_LOCK), rootLock);
    writeJson(join(root, HIDDEN_LOCK), hiddenLock);
    writeJson(join(packageRoot, "node_modules/nested/package.json"), {
      name: "nested",
      version: "1.0.0",
      license: "MIT",
    });
    writeFile(join(packageRoot, "node_modules/nested/LICENSE"), "nested license\n");
    rewriteStaticManifest(root);
    const packageFiles = packageFilesFor(packageRoot, [
      ".hidden",
      "LICENSE.txt",
      "bin/tool",
      "node_modules/nested/LICENSE",
      "node_modules/nested/package.json",
      "package.json",
    ]);
    writeStaticAuthority(root, {
      supplementalLicenseTexts: [],
      packageBindings: [licenseBinding(root, { packageFiles })],
    });

    const result = capture(root);
    const alias = result.npm.packages.find(
      ({ physicalPath }) => physicalPath === "node_modules/alias-package",
    );
    expect(alias).toMatchObject({
      packageContentVerification: "PASS",
      packageFiles,
    });
    expect(alias?.packageFiles).toHaveLength(6);
    expect(
      result.npm.packages.find(
        ({ physicalPath }) => physicalPath === "node_modules/alias-package/node_modules/nested",
      ),
    ).toMatchObject({ packageContentVerification: "NOT_RUN", packageFiles: null });
  });

  it("rejects package file tamper, omission, extras, and symlinks", () => {
    const tampered = createFixture("package-files-tampered");
    const missing = createFixture("package-files-missing");
    const extra = createFixture("package-files-extra");
    const symlink = createFixture("package-files-symlink");

    for (const root of [tampered, missing, extra, symlink]) {
      const packageRoot = join(root, TOOLS, "node_modules/alias-package");
      writeFile(join(packageRoot, ".hidden"), "hidden\n");
      mkdirSync(join(packageRoot, "bin"), { recursive: true });
      writeFileSync(join(packageRoot, "bin/tool"), Buffer.alloc(2 * 1024 * 1024, 7));
      rewriteStaticManifest(root);
      writeStaticAuthority(root, {
        supplementalLicenseTexts: [],
        packageBindings: [
          licenseBinding(root, {
            packageFiles: packageFilesFor(packageRoot, [
              ".hidden",
              "LICENSE.txt",
              "bin/tool",
              "package.json",
            ]),
          }),
        ],
      });
    }

    writeFileSync(
      join(tampered, TOOLS, "node_modules/alias-package/bin/tool"),
      Buffer.alloc(2 * 1024 * 1024, 8),
    );
    expect(() => capture(tampered)).toThrow(/package file bin\/tool sha256/u);

    rmSync(join(missing, TOOLS, "node_modules/alias-package/.hidden"));
    expect(() => capture(missing)).toThrow(/package file set.*missing/u);

    writeFile(join(extra, TOOLS, "node_modules/alias-package/extra.bin"), "extra\n");
    expect(() => capture(extra)).toThrow(/package file set.*extra/u);

    writeFile(join(symlink, "outside-package-file"), "outside\n");
    rmSync(join(symlink, TOOLS, "node_modules/alias-package/.hidden"));
    symlinkSync(
      "../../../../outside-package-file",
      join(symlink, TOOLS, "node_modules/alias-package/.hidden"),
    );
    expect(() => capture(symlink)).toThrow(/package file .*symbolic link/u);
  });

  it("rejects empty, unsafe, colliding, oversized, and incomplete package file metadata", () => {
    const empty = createFixture("package-files-empty");
    const unsafe = createFixture("package-files-unsafe");
    const colliding = createFixture("package-files-colliding");
    const oversized = createFixture("package-files-oversized");
    const incomplete = createFixture("package-files-incomplete");

    for (const [root, packageFiles, expected] of [
      [empty, [], /packageFiles must not be empty/u],
      [unsafe, [{ path: "../escape", sha256: "0".repeat(64), size: 1 }], /safe relative/u],
      [
        colliding,
        [
          { path: "package.json", sha256: "0".repeat(64), size: 1 },
          { path: "package.json/nested", sha256: "0".repeat(64), size: 1 },
        ],
        /packageFiles.*ancestor/u,
      ],
      [
        oversized,
        [{ path: "package.json", sha256: "0".repeat(64), size: 64 * 1024 * 1024 + 1 }],
        /packageFiles.*size/u,
      ],
      [
        incomplete,
        [{ path: "README.md", sha256: "0".repeat(64), size: 1 }],
        /packageFiles must contain package\.json/u,
      ],
    ] as const) {
      writeStaticAuthority(root, {
        supplementalLicenseTexts: [],
        packageBindings: [licenseBinding(root, { packageFiles })],
      });
      expect(() => capture(root)).toThrow(expected);
    }
  });

  it("requires current-platform package files to be installed but permits opposite-arch absence", () => {
    const current = createFixture("package-files-current-absent");
    const opposite = createFixture("package-files-opposite-absent");

    for (const [root, path, cpu] of [
      [current, "node_modules/current-optional", "arm64"],
      [opposite, "node_modules/opposite-optional", "x64"],
    ] as const) {
      const record = {
        name: path.slice("node_modules/".length),
        version: "2.0.0",
        resolved: `https://registry.example/${path.slice("node_modules/".length)}.tgz`,
        integrity: `sha512-${Buffer.alloc(64, 4).toString("base64")}`,
        license: "MIT",
        os: ["linux"],
        cpu: [cpu],
      };
      const lock = readJson(join(root, ROOT_LOCK));
      lock.packages[path] = record;
      writeJson(join(root, ROOT_LOCK), lock);
      rewriteStaticManifest(root);
      writeStaticAuthority(root, {
        supplementalLicenseTexts: [],
        packageBindings: [
          licenseBinding(root, {
            path,
            packageFiles: [{ path: "package.json", sha256: "0".repeat(64), size: 1 }],
          }),
        ],
      });
    }

    expect(() => capture(current)).toThrow(/packageFiles binding.*current platform.*installed/u);
    expect(capture(opposite).npm.status).toBe("PASS");
    expect(capture(opposite).npm.packages).toHaveLength(1);
  });

  it("rejects a bundled README hash or package binding that drifts from the root lock", () => {
    const bundled = createFixture("bundled-readme-tampered");
    const wrongName = createFixture("binding-wrong-name");
    const wrongVersion = createFixture("binding-wrong-version");
    const wrongIntegrity = createFixture("binding-wrong-integrity");

    const packageRoot = join(bundled, TOOLS, "node_modules/alias-package");
    rmSync(join(packageRoot, "LICENSE.txt"));
    writeFile(join(packageRoot, "README.md"), "expected README\n");
    writeStaticAuthority(bundled, {
      supplementalLicenseTexts: [],
      packageBindings: [
        licenseBinding(bundled, {
          bundledLicenseFiles: [{ path: "README.md", sha256: sha256(Buffer.from("other\n")) }],
        }),
      ],
    });
    expect(() => capture(bundled)).toThrow(/bundled license file README\.md sha256/u);

    writeStaticAuthority(wrongName, {
      supplementalLicenseTexts: [],
      packageBindings: [licenseBinding(wrongName, { name: "wrong" })],
    });
    expect(() => capture(wrongName)).toThrow(/packageBindings.*name.*package-lock/u);

    writeStaticAuthority(wrongVersion, {
      supplementalLicenseTexts: [],
      packageBindings: [licenseBinding(wrongVersion, { version: "9.9.9" })],
    });
    expect(() => capture(wrongVersion)).toThrow(/packageBindings.*version.*package-lock/u);

    writeStaticAuthority(wrongIntegrity, {
      supplementalLicenseTexts: [],
      packageBindings: [
        licenseBinding(wrongIntegrity, {
          integrity: `sha512-${Buffer.alloc(64, 8).toString("base64")}`,
        }),
      ],
    });
    expect(() => capture(wrongIntegrity)).toThrow(/packageBindings.*integrity.*package-lock/u);
  });

  it("rejects missing, tampered, or symlinked supplemental files", () => {
    const missing = createFixture("supplemental-missing");
    const tampered = createFixture("supplemental-tampered");
    const symlink = createFixture("supplemental-symlink");

    const content = "expected\n";
    for (const root of [missing, tampered, symlink]) {
      writeStaticAuthority(root, {
        supplementalLicenseTexts: [supplementalAuthority(root, content)],
        packageBindings: [
          licenseBinding(root, { supplementalLicenseTextIds: ["fixture-supplemental"] }),
        ],
      });
    }
    expect(() => capture(missing)).toThrow(/supplemental license text.*missing/u);

    writeFile(
      join(tampered, "usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/fixture.txt"),
      "tampered\n",
    );
    expect(() => capture(tampered)).toThrow(/supplemental license text.*sha256/u);

    writeFile(join(symlink, "usr/share/doc/cloud-agents/outside.txt"), content);
    mkdirSync(join(symlink, "usr/share/doc/cloud-agents/worker-oci-supplemental-licenses"), {
      recursive: true,
    });
    symlinkSync(
      "../outside.txt",
      join(symlink, "usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/fixture.txt"),
    );
    expect(() => capture(symlink)).toThrow(/supplemental-licenses.*symbolic link/u);
  });

  it("rejects unknown, duplicate, and escaping license bindings", () => {
    const unknown = createFixture("binding-unknown");
    const duplicate = createFixture("binding-duplicate");
    const escaping = createFixture("binding-escaping");

    writeStaticAuthority(unknown, {
      supplementalLicenseTexts: [],
      packageBindings: [licenseBinding(unknown, { supplementalLicenseTextIds: ["unknown"] })],
    });
    expect(() => capture(unknown)).toThrow(/unknown supplemental license text id/u);

    const binding = licenseBinding(duplicate, {});
    writeStaticAuthority(duplicate, {
      supplementalLicenseTexts: [],
      packageBindings: [binding, binding],
    });
    expect(() => capture(duplicate)).toThrow(/duplicate package binding path/u);

    writeStaticAuthority(escaping, {
      supplementalLicenseTexts: [],
      packageBindings: [
        licenseBinding(escaping, {
          bundledLicenseFiles: [{ path: "../escape", sha256: "0".repeat(64) }],
        }),
      ],
    });
    expect(() => capture(escaping)).toThrow(/bundledLicenseFiles.*path.*safe relative/u);
  });

  it("includes held installed Debian packages and rejects broken installed state", () => {
    const held = createFixture("held-debian");
    const broken = createFixture("broken-debian");

    writeFileSync(
      join(held, "var/lib/dpkg/status"),
      dpkgStatus("arm64").replace("install ok installed", "hold ok installed"),
    );
    expect(capture(held).debian.packages).toHaveLength(1);

    writeFileSync(
      join(broken, "var/lib/dpkg/status"),
      dpkgStatus("arm64").replace("install ok installed", "install reinstreq installed"),
    );
    expect(() => capture(broken)).toThrow(/broken installed state/u);
  });
});

function capture(root: string) {
  return captureWorkerOciInstalledInventory(root, "linux/arm64", {
    platform: "linux",
    arch: "arm64",
  });
}

function createFixture(name: string): string {
  const root = mkdtempSync(join(tmpdir(), `cloud-agents-worker-${name}-`));
  onTestFinished(() => rmSync(root, { recursive: true, force: true }));
  const packageManifestPath = join(root, TOOLS, "package.json");
  const rootLockPath = join(root, ROOT_LOCK);
  const hiddenLockPath = join(root, HIDDEN_LOCK);
  const packageJsonPath = join(root, TOOLS, "node_modules/alias-package/package.json");

  writeJson(packageManifestPath, {
    name: "fixture-worker-tools",
    private: true,
    dependencies: { "alias-package": "1.0.0" },
  });
  const packageRecord = {
    name: "actual-package",
    version: "1.0.0",
    resolved: "https://registry.example/actual-package.tgz",
    integrity: `sha512-${Buffer.alloc(64, 7).toString("base64")}`,
    license: "SEE LICENSE IN LICENSE.txt",
    os: ["linux"],
    cpu: ["arm64"],
  };
  writeJson(rootLockPath, {
    name: "fixture-worker-tools",
    lockfileVersion: 3,
    packages: {
      "": { dependencies: { "alias-package": "1.0.0" } },
      "node_modules/alias-package": packageRecord,
    },
  });
  writeJson(hiddenLockPath, {
    name: "fixture-worker-tools",
    lockfileVersion: 3,
    packages: { "node_modules/alias-package": packageRecord },
  });
  writeJson(packageJsonPath, {
    name: "actual-package",
    version: "1.0.0",
    license: "SEE LICENSE IN LICENSE.txt",
  });
  writeFile(join(root, TOOLS, "node_modules/alias-package/LICENSE.txt"), "license text\n");
  writeFile(join(root, "var/lib/dpkg/status"), dpkgStatus("arm64"));
  writeFile(
    join(root, "usr/share/doc/fixture-package/copyright"),
    "Copyright: fixture\nLicense: GPL-3+\n",
  );
  writeFile(join(root, "usr/share/common-licenses/GPL-3"), "common license\n");
  rewriteStaticManifest(root);
  return root;
}

function rewriteStaticManifest(root: string): void {
  writeJson(join(root, STATIC_MANIFEST), {
    schemaVersion: 2,
    kind: "cloud-agents-worker-oci-install-manifest",
    source: {
      packageManifestSha256: sha256(readFileSync(join(root, `${TOOLS}/package.json`))),
      lockfileSha256: sha256(readFileSync(join(root, ROOT_LOCK))),
      installedInventoryCollectorSha256: sha256(readFileSync(COLLECTOR)),
    },
    supplementalLicenseTexts: [],
    packageBindings: [],
  });
}

type StaticAuthorityOverrides = {
  supplementalLicenseTexts?: Array<Record<string, unknown>>;
  packageBindings?: Array<Record<string, unknown>>;
};

function writeStaticAuthority(root: string, overrides: StaticAuthorityOverrides): void {
  const manifest = readJson(join(root, STATIC_MANIFEST));
  manifest.supplementalLicenseTexts = overrides.supplementalLicenseTexts ?? [];
  manifest.packageBindings = overrides.packageBindings ?? [];
  writeJson(join(root, STATIC_MANIFEST), manifest);
}

function supplementalAuthority(
  root: string,
  content: string,
  options: { id?: string; suffix?: string } = {},
): Record<string, unknown> {
  const id = options.id ?? "fixture-supplemental";
  const suffix = options.suffix ?? "fixture.txt";
  return {
    id,
    artifactPath: `deploy/docker/worker-oci-supplemental-licenses/${suffix}`,
    sha256: sha256(Buffer.from(content)),
    imagePath: `/usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/${suffix}`,
    source: {
      kind: "repository",
      repository: "https://example.invalid/cloud-agents",
      commit: "0123456789abcdef0123456789abcdef01234567",
      path: "licenses/fixture.txt",
    },
  };
}

function licenseBinding(
  root: string,
  overrides: {
    path?: string;
    name?: string;
    version?: string;
    integrity?: string;
    supplementalLicenseTextIds?: string[];
    bundledLicenseFiles?: Array<{ path: string; sha256: string }>;
    packageFiles?: ReadonlyArray<{ path: string; sha256: string; size: number }>;
  },
): Record<string, unknown> {
  const lock = readJson(join(root, ROOT_LOCK));
  const record = lock.packages[overrides.path ?? "node_modules/alias-package"];
  return {
    path: overrides.path ?? "node_modules/alias-package",
    name: overrides.name ?? record.name,
    version: overrides.version ?? record.version,
    integrity: overrides.integrity ?? record.integrity,
    supplementalLicenseTextIds: overrides.supplementalLicenseTextIds ?? [],
    bundledLicenseFiles: overrides.bundledLicenseFiles ?? [],
    ...(overrides.packageFiles === undefined ? {} : { packageFiles: overrides.packageFiles }),
  };
}

function packageFilesFor(
  packageRoot: string,
  paths: string[],
): Array<{ path: string; sha256: string; size: number }> {
  return paths
    .map((path) => {
      const bytes = readFileSync(join(packageRoot, path));
      return { path, sha256: sha256(bytes), size: bytes.length };
    })
    .toSorted((left, right) => (left.path < right.path ? -1 : left.path > right.path ? 1 : 0));
}

function dpkgStatus(architecture: string): string {
  return [
    "Package: fixture-package",
    "Status: install ok installed",
    "Priority: required",
    "Architecture: " + architecture,
    "Source: fixture-source (1.2.3-1)",
    "Version: 1.2.3-1",
    "Description: fixture",
    "",
  ].join("\n");
}

function writeFile(path: string, value: string): void {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, value);
}

function writeJson(path: string, value: unknown): void {
  writeFile(path, `${JSON.stringify(value, null, 2)}\n`);
}

function readJson(path: string): any {
  return JSON.parse(readFileSync(path, "utf8"));
}

function sha256(value: Buffer): string {
  return createHash("sha256").update(value).digest("hex");
}
