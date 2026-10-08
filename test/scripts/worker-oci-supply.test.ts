import { createHash } from "node:crypto";
import {
  copyFileSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import {
  buildWorkerOciSupplyArtifacts,
  WORKER_OCI_INSTALL_MANIFEST_FILENAME,
  WORKER_OCI_NOTICES_FILENAME,
} from "../../scripts/lib/worker-oci-supply";

const PACKAGE_MANIFEST = "deploy/docker/worker-tools/package.json";
const PACKAGE_LOCK = "deploy/docker/worker-tools/package-lock.json";
const AUTHORITY = "deploy/docker/worker-oci-package-authority.json";
const ARTIFACT = "deploy/docker/worker-oci-supplemental-licenses/sdk-license.txt";
const TRANSITIVE_PATH = "node_modules/@agentclientprotocol/sdk";

describe("Worker OCI npm and license authority", () => {
  it("carries authority blockers into the bound manifest and notice", () => {
    const root = copyWorkerOciSupplyFixture("authority-blockers");
    try {
      const before = buildWorkerOciSupplyArtifacts(root);
      const authority = readJson(join(root, AUTHORITY));
      const reason = "The bound native package has no final static-link component record.";
      authority.blockedReasons = [reason];
      writeJson(join(root, AUTHORITY), authority);
      const result = buildWorkerOciSupplyArtifacts(root);
      expect(result.manifest.status).toBe("BLOCKED");
      expect(result.manifest.transitiveClosure).toBe("NOT_PROVEN");
      expect(result.manifest.blockedReasons).toEqual([...before.manifest.blockedReasons, reason]);
      expect(result.noticeBytes.toString("utf8")).toContain(`- BLOCKED: ${reason}`);
      expect(result.manifest.packageBindings).toEqual(before.manifest.packageBindings);
      expect(result.manifest.source.packageAuthoritySha256).toBe(
        sha256(readFileSync(join(root, AUTHORITY))),
      );
      expect(result.manifest.source.packageAuthoritySha256).not.toBe(
        before.manifest.source.packageAuthoritySha256,
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it.each(
    [
      null,
      "not an array",
      [1],
      [""],
      [" "],
      ["line\nbreak"],
      ["control\u0000"],
      ["duplicate", "duplicate"],
    ].map((blockedReasons) => ({
      blockedReasons,
    })),
  )("rejects malformed authority blockers: %j", ({ blockedReasons }) => {
    const root = copyWorkerOciSupplyFixture("invalid-authority-blockers");
    try {
      const authority = readJson(join(root, AUTHORITY));
      authority.blockedReasons = blockedReasons;
      writeJson(join(root, AUTHORITY), authority);
      expect(() => buildWorkerOciSupplyArtifacts(root)).toThrow(/blockedReasons/u);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("captures deterministic schema-v2 materials and keeps the old closure boundary", () => {
    const root = copyWorkerOciSupplyFixture("deterministic");
    try {
      const first = buildWorkerOciSupplyArtifacts(root);
      const second = buildWorkerOciSupplyArtifacts(root);
      expect(first.manifestBytes).toEqual(second.manifestBytes);
      expect(first.noticeBytes).toEqual(second.noticeBytes);
      expect(first.manifest).toMatchObject({
        kind: "cloud-agents-worker-oci-install-manifest",
        status: "BLOCKED",
        transitiveClosure: "NOT_PROVEN",
        directInstalls: expect.arrayContaining([
          expect.objectContaining({ package: "@openai/codex", version: "0.154.0" }),
        ]),
        supplementalLicenseTexts: [
          expect.objectContaining({
            id: "sdk-license",
            imagePath:
              "/usr/share/doc/cloud-agents/worker-oci-supplemental-licenses/sdk-license.txt",
          }),
        ],
        packageBindings: [
          expect.objectContaining({
            path: TRANSITIVE_PATH,
            name: "@agentclientprotocol/sdk",
            supplementalLicenseTextIds: ["sdk-license"],
          }),
        ],
      });
      expect(
        first.manifest.packageBindings.find((item) => item.path === TRANSITIVE_PATH),
      ).not.toHaveProperty("packageFiles");
      expect(first.manifest.directInstalls).toHaveLength(5);
      expect(first.manifest.blockedReasons).toContain(
        "The apt package closure and license inventory are not bound by repository evidence.",
      );
      expect(first.manifest.blockedReasons).toContain(
        "The npm lock binds fetched bytes, but the installed platform-specific closure and its license inventory are not proven.",
      );
      expect(first.sourceFiles.get(ARTIFACT)?.toString()).toBe("Apache License material\n");
      expect([...first.sourceFiles.keys()]).toEqual([...first.sourceFiles.keys()].toSorted());
      expect(first.noticeBytes.toString("utf8")).toContain("do not constitute legal approval");
      expect(first.noticeBytes.toString("utf8")).toContain("sdk-license");
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("accepts npm alias names when the authority path and lock record agree", () => {
    const root = copyWorkerOciSupplyFixture("alias");
    try {
      const authority = readJson(join(root, AUTHORITY));
      const lock = readJson(join(root, PACKAGE_LOCK));
      const path = "node_modules/@openai/codex-linux-arm64";
      const entry = lock.packages[path];
      authority.packages.push({
        name: entry.name,
        path,
        version: entry.version,
        sourceUrl: entry.resolved,
        integrity: entry.integrity,
        license: entry.license,
        supplementalLicenseTextIds: ["sdk-license"],
        bundledLicenseFiles: [],
      });
      writeJson(join(root, AUTHORITY), authority);
      const result = buildWorkerOciSupplyArtifacts(root);
      expect(result.manifest.packageBindings).toContainEqual(
        expect.objectContaining({ path, name: "@openai/codex" }),
      );
      expect(result.manifest.blockedReasons).not.toContain(
        `${path} package authority is not a direct package.json dependency and has no supplemental or bundled license reference.`,
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("requires a direct record with license references to match the lock strictly", () => {
    const root = copyWorkerOciSupplyFixture("direct-license-drift");
    try {
      const authority = readJson(join(root, AUTHORITY));
      const direct = authority.packages.find((item: JsonRecord) => item.name === "@openai/codex");
      direct.supplementalLicenseTextIds = ["sdk-license"];
      direct.sourceUrl = "https://registry.example.invalid/codex.tgz";
      writeJson(join(root, AUTHORITY), authority);
      expect(() => buildWorkerOciSupplyArtifacts(root)).toThrow(
        /package authority sourceUrl does not match package-lock\.json/u,
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("requires an npm alias authority name to equal lock.name", () => {
    const root = copyWorkerOciSupplyFixture("alias-name-drift");
    try {
      const authority = readJson(join(root, AUTHORITY));
      const lock = readJson(join(root, PACKAGE_LOCK));
      const path = "node_modules/@openai/codex-linux-arm64";
      const entry = lock.packages[path];
      authority.packages.push({
        name: "@openai/codex-linux-arm64",
        path,
        version: entry.version,
        sourceUrl: entry.resolved,
        integrity: entry.integrity,
        license: entry.license,
        supplementalLicenseTextIds: ["sdk-license"],
        bundledLicenseFiles: [],
      });
      writeJson(join(root, AUTHORITY), authority);
      expect(() => buildWorkerOciSupplyArtifacts(root)).toThrow(
        /package authority name .* does not match package-lock\.json name @openai\/codex/u,
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("accepts fixed source-archive provenance and rejects credentials, traversal, and bad digests", () => {
    const valid = copyWorkerOciSupplyFixture("source-archive-valid");
    try {
      const authority = readJson(join(valid, AUTHORITY));
      authority.supplementalLicenseTexts[0].source = {
        kind: "source-archive",
        url: "https://download.gnome.org/sources/glib/2.90/glib-2.90.0.tar.xz",
        sha256: "a".repeat(64),
        member: "glib-2.90.0/COPYING",
      };
      writeJson(join(valid, AUTHORITY), authority);
      const result = buildWorkerOciSupplyArtifacts(valid);
      expect(result.manifest.supplementalLicenseTexts[0]?.source).toEqual({
        kind: "source-archive",
        url: "https://download.gnome.org/sources/glib/2.90/glib-2.90.0.tar.xz",
        sha256: "a".repeat(64),
        member: "glib-2.90.0/COPYING",
      });
      expect(result.noticeBytes.toString("utf8")).toContain("source archive");
      expect(result.noticeBytes.toString("utf8")).toContain("a".repeat(64));
    } finally {
      rmSync(valid, { recursive: true, force: true });
    }

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
        /source path is invalid/u,
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
    for (const [name, source, expected] of cases) {
      const root = copyWorkerOciSupplyFixture(name);
      try {
        const authority = readJson(join(root, AUTHORITY));
        authority.supplementalLicenseTexts[0].source = source;
        writeJson(join(root, AUTHORITY), authority);
        expect(() => buildWorkerOciSupplyArtifacts(root)).toThrow(expected);
      } finally {
        rmSync(root, { recursive: true, force: true });
      }
    }
  });

  it("accepts a packageFiles-only transitive binding and emits an immutable sorted set", () => {
    const root = copyWorkerOciSupplyFixture("package-files");
    try {
      const authority = readJson(join(root, AUTHORITY));
      authority.supplementalLicenseTexts = [];
      const transitive = authority.packages.find(
        (item: JsonRecord) => item.path === TRANSITIVE_PATH,
      );
      transitive.supplementalLicenseTextIds = [];
      transitive.packageFiles = [
        packageFile("versions.json", 7, "b"),
        packageFile("package.json", 11, "a"),
      ];
      writeJson(join(root, AUTHORITY), authority);
      const result = buildWorkerOciSupplyArtifacts(root);
      const binding = result.manifest.packageBindings.find((item) => item.path === TRANSITIVE_PATH);
      expect(binding?.packageFiles).toEqual([
        packageFile("package.json", 11, "a"),
        packageFile("versions.json", 7, "b"),
      ]);
      expect(result.noticeBytes.toString("utf8")).toContain("Bound package member files");
      expect(result.noticeBytes.toString("utf8")).toContain("tarball-to-installed-byte check");
      expect(JSON.parse(result.manifestBytes.toString("utf8")).schemaVersion).toBe(2);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects empty, incomplete, duplicate, colliding, and oversized package file sets", () => {
    const cases: Array<[string, (files: JsonRecord[]) => void, RegExp]> = [
      ["empty", (files) => files.splice(0), /non-empty array/u],
      [
        "missing-package-json",
        (files) => files.splice(0, files.length, packageFile("README.md", 1)),
        /contain package\.json/u,
      ],
      [
        "duplicate",
        (files) => files.push(packageFile("package.json", 2)),
        /duplicate package file path/u,
      ],
      [
        "ancestry-collision",
        (files) => files.push(packageFile("package.json/nested", 2)),
        /ancestry collision/u,
      ],
      [
        "too-large",
        (files) => (files[0] = packageFile("package.json", 64 * 1024 * 1024 + 1)),
        /size must be an integer/u,
      ],
      [
        "fractional-size",
        (files) => (files[0] = { ...packageFile("package.json", 1), size: 1.5 }),
        /size must be an integer/u,
      ],
      [
        "negative-size",
        (files) => (files[0] = packageFile("package.json", -1)),
        /size must be an integer/u,
      ],
      [
        "invalid-sha256",
        (files) => (files[0] = { ...packageFile("package.json", 1), sha256: "not-a-digest" }),
        /must be a lowercase SHA-256 digest/u,
      ],
      [
        "unsafe-path",
        (files) => (files[0] = packageFile("../package.json", 1)),
        /package file path is invalid/u,
      ],
    ];
    for (const [name, mutate, expected] of cases) {
      const root = copyWorkerOciSupplyFixture(`package-files-${name}`);
      try {
        const authority = readJson(join(root, AUTHORITY));
        const transitive = authority.packages.find(
          (item: JsonRecord) => item.path === TRANSITIVE_PATH,
        );
        transitive.packageFiles = [packageFile("package.json", 1)];
        mutate(transitive.packageFiles);
        writeJson(join(root, AUTHORITY), authority);
        expect(() => buildWorkerOciSupplyArtifacts(root)).toThrow(expected);
      } finally {
        rmSync(root, { recursive: true, force: true });
      }
    }
  });

  it("requires direct packageFiles bindings to match lock metadata strictly", () => {
    const root = copyWorkerOciSupplyFixture("direct-package-files-drift");
    try {
      const authority = readJson(join(root, AUTHORITY));
      const direct = authority.packages.find((item: JsonRecord) => item.name === "@openai/codex");
      direct.packageFiles = [packageFile("package.json", 1)];
      direct.integrity = "sha512-AAAA";
      writeJson(join(root, AUTHORITY), authority);
      expect(() => buildWorkerOciSupplyArtifacts(root)).toThrow(
        /package authority integrity does not match package-lock\.json/u,
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("keeps direct invalid-SRI behavior bounded by NOASSERTION", () => {
    const root = copyWorkerOciSupplyFixture("direct-invalid-sri");
    try {
      const lock = readJson(join(root, PACKAGE_LOCK));
      const authority = readJson(join(root, AUTHORITY));
      const packageName = "@openai/codex";
      lock.packages[`node_modules/${packageName}`].integrity = "sha512-AAAA";
      authority.packages.find((item: JsonRecord) => item.name === packageName).integrity =
        "sha512-AAAA";
      writeJson(join(root, PACKAGE_LOCK), lock);
      writeJson(join(root, AUTHORITY), authority);
      const result = buildWorkerOciSupplyArtifacts(root);
      expect(result.manifest.directInstalls).toContainEqual(
        expect.objectContaining({
          package: packageName,
          sourceUrl: "NOASSERTION",
          integrity: "NOASSERTION",
        }),
      );
      expect(result.manifest.blockedReasons).toContain(
        `${packageName}@0.154.0 package-lock.json integrity is not canonical SHA-512 SRI.`,
      );
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("rejects unknown, unreferenced, duplicate, and escaped authority material", () => {
    const cases: Array<[string, (authority: JsonRecord) => void, RegExp]> = [
      [
        "unknown-reference",
        (authority) => {
          authority.packages[0].supplementalLicenseTextIds = ["missing"];
        },
        /unknown supplemental license text/u,
      ],
      [
        "unreferenced-text",
        (authority) => {
          authority.supplementalLicenseTexts.push({
            ...authority.supplementalLicenseTexts[0],
            id: "unused",
            artifactPath: "deploy/docker/worker-oci-supplemental-licenses/unused.txt",
          });
        },
        /unreferenced/u,
      ],
      [
        "escaped-artifact",
        (authority) => {
          authority.supplementalLicenseTexts[0].artifactPath =
            "deploy/docker/worker-oci-supplemental-licenses/../x";
        },
        /artifact path .*invalid/u,
      ],
      [
        "duplicate-path",
        (authority) => {
          authority.packages.push({ ...authority.packages[0] });
        },
        /duplicate path/u,
      ],
      [
        "unbound-transitive",
        (authority) => {
          authority.packages.find(
            (item: JsonRecord) => item.path === TRANSITIVE_PATH,
          ).supplementalLicenseTextIds = [];
        },
        /no supplemental or bundled license reference/u,
      ],
    ];
    for (const [name, mutate, expected] of cases) {
      const root = copyWorkerOciSupplyFixture(name);
      try {
        const authority = readJson(join(root, AUTHORITY));
        mutate(authority);
        writeJson(join(root, AUTHORITY), authority);
        expect(() => buildWorkerOciSupplyArtifacts(root)).toThrow(expected);
      } finally {
        rmSync(root, { recursive: true, force: true });
      }
    }
  });

  it("rejects invalid source metadata and symlinked or changed material bytes", () => {
    const invalidSource = copyWorkerOciSupplyFixture("invalid-source");
    try {
      const authority = readJson(join(invalidSource, AUTHORITY));
      authority.supplementalLicenseTexts[0].source.repository = "http://github.com/example/repo";
      writeJson(join(invalidSource, AUTHORITY), authority);
      expect(() => buildWorkerOciSupplyArtifacts(invalidSource)).toThrow(/HTTPS URL/u);
    } finally {
      rmSync(invalidSource, { recursive: true, force: true });
    }

    const symlinked = copyWorkerOciSupplyFixture("symlinked");
    try {
      rmSync(join(symlinked, ARTIFACT));
      symlinkSync("../../../../README.md", join(symlinked, ARTIFACT));
      expect(() => buildWorkerOciSupplyArtifacts(symlinked)).toThrow(/symlink/u);
    } finally {
      rmSync(symlinked, { recursive: true, force: true });
    }

    const changed = copyWorkerOciSupplyFixture("changed-bytes");
    try {
      const authority = readJson(join(changed, AUTHORITY));
      authority.supplementalLicenseTexts[0].sha256 = sha256(Buffer.from("different\n"));
      writeJson(join(changed, AUTHORITY), authority);
      expect(() => buildWorkerOciSupplyArtifacts(changed)).toThrow(/SHA-256 does not match/u);
    } finally {
      rmSync(changed, { recursive: true, force: true });
    }
  });

  it("captures each source byte once and deduplicates shared references", () => {
    const root = copyWorkerOciSupplyFixture("capture-once");
    try {
      const authority = readJson(join(root, AUTHORITY));
      const lock = readJson(join(root, PACKAGE_LOCK));
      const secondPath = "node_modules/@anthropic-ai/sdk";
      const entry = lock.packages[secondPath];
      authority.packages.push({
        name: "@anthropic-ai/sdk",
        path: secondPath,
        version: entry.version,
        sourceUrl: entry.resolved,
        integrity: entry.integrity,
        license: entry.license,
        supplementalLicenseTextIds: ["sdk-license"],
        bundledLicenseFiles: [],
      });
      writeJson(join(root, AUTHORITY), authority);
      const result = buildWorkerOciSupplyArtifacts(root);
      const captured = result.sourceFiles.get(ARTIFACT);
      expect(captured).toEqual(Buffer.from("Apache License material\n"));
      writeFileSync(join(root, ARTIFACT), "changed after capture\n");
      expect(captured).toEqual(Buffer.from("Apache License material\n"));
      expect([...result.sourceFiles.keys()].filter((path) => path === ARTIFACT)).toHaveLength(1);
      expect(
        result.manifest.packageBindings.filter((item) =>
          item.supplementalLicenseTextIds.includes("sdk-license"),
        ),
      ).toHaveLength(2);
    } finally {
      rmSync(root, { recursive: true, force: true });
    }
  });

  it("keeps generated artifact names and the supplemental Docker copy requirement explicit", () => {
    expect(WORKER_OCI_INSTALL_MANIFEST_FILENAME).toBe(
      "cloud-agents-worker-oci-install-manifest.json",
    );
    expect(WORKER_OCI_NOTICES_FILENAME).toBe("cloud-agents-worker-oci-notices.md");
  });
});

type JsonRecord = Record<string, any>;

function copyWorkerOciSupplyFixture(name: string): string {
  const root = mkdtempSync(join(tmpdir(), `worker-oci-supply-${name}-`));
  mkdirSync(join(root, "deploy/docker/worker-tools"), { recursive: true });
  mkdirSync(join(root, "deploy/docker/worker-oci-supplemental-licenses"), { recursive: true });
  mkdirSync(join(root, "scripts/lib"), { recursive: true });
  for (const path of [
    "deploy/docker/worker.Dockerfile",
    PACKAGE_MANIFEST,
    PACKAGE_LOCK,
    "scripts/lib/worker-oci-installed.ts",
  ]) {
    copyFileSync(path, join(root, path));
  }
  const lock = readJson(join(root, PACKAGE_LOCK));
  const packages = Object.entries({
    ...lock.packages[""].dependencies,
    ...lock.packages[""].optionalDependencies,
  }).map(([name, version]) => {
    const path = `node_modules/${name}`;
    const entry = lock.packages[path];
    return {
      name,
      path,
      version,
      sourceUrl: entry.resolved,
      integrity: entry.integrity,
      license: entry.license,
      supplementalLicenseTextIds: [],
      bundledLicenseFiles: [],
    };
  });
  const artifactBytes = Buffer.from("Apache License material\n");
  writeFileSync(join(root, ARTIFACT), artifactBytes);
  packages.push({
    name: "@agentclientprotocol/sdk",
    path: TRANSITIVE_PATH,
    version: lock.packages[TRANSITIVE_PATH].version,
    sourceUrl: lock.packages[TRANSITIVE_PATH].resolved,
    integrity: lock.packages[TRANSITIVE_PATH].integrity,
    license: lock.packages[TRANSITIVE_PATH].license,
    supplementalLicenseTextIds: ["sdk-license"],
    bundledLicenseFiles: [],
  });
  writeJson(join(root, AUTHORITY), {
    schemaVersion: 2,
    registry: "https://registry.npmmirror.com/",
    packages,
    supplementalLicenseTexts: [
      {
        id: "sdk-license",
        artifactPath: ARTIFACT,
        sha256: sha256(artifactBytes),
        source: {
          kind: "repository",
          repository: "https://github.com/agentclientprotocol/typescript-sdk",
          commit: "0123456789abcdef0123456789abcdef01234567",
          path: "LICENSE",
        },
      },
    ],
  });
  return root;
}

function readJson(path: string): JsonRecord {
  return JSON.parse(requireText(path)) as JsonRecord;
}

function writeJson(path: string, value: JsonRecord): void {
  writeFileSync(path, `${JSON.stringify(value, null, 2)}\n`);
}

function requireText(path: string): string {
  return readFileSync(path, "utf8");
}

function sha256(value: Buffer): string {
  return createHash("sha256").update(value).digest("hex");
}

function packageFile(path: string, size: number, fill = "a"): JsonRecord {
  return { path, sha256: fill.repeat(64), size };
}
