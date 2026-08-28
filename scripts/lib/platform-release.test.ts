import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";

import { readDeterministicUstar } from "./platform-migration-ustar";
import {
  expectedArtifactIdentities,
  buildPlatformMigrationPackage,
  buildPlatformDeploymentPackage,
  parsePlatformReleaseOptions,
  platformReleaseArtifact,
  validatePlatformReleaseManifest,
} from "./platform-release";

describe("platform release", () => {
  it("requires a version and a new output directory", () => {
    expect(() => parsePlatformReleaseOptions([])).toThrow(/Usage/);
    expect(() =>
      parsePlatformReleaseOptions(["--version", "not-semver", "--output-dir", "out"]),
    ).toThrow(/Usage/);
    expect(
      parsePlatformReleaseOptions(["--version", "0.1.0-rc.1", "--output-dir", "out"], "/tmp"),
    ).toEqual({
      version: "0.1.0-rc.1",
      outputDirectory: "/tmp/out",
      allowDirty: false,
    });
  });

  it("binds artifact-level size and sha256 only", () => {
    const bytes = new TextEncoder().encode("artifact");
    const artifact = platformReleaseArtifact(
      "control-plane",
      "linux-amd64",
      "control-plane-linux-amd64",
      bytes,
    );
    expect(artifact.sizeBytes).toBe(bytes.byteLength);
    expect(artifact.sha256).toBe(`sha256:${createHash("sha256").update(bytes).digest("hex")}`);
  });

  it("accepts the exact platform artifact set and rejects drift", () => {
    const artifacts = expectedArtifactIdentities().map(({ name, target }) => ({
      name,
      target,
      filename: `${name}-${target}`,
      sizeBytes: 1,
      sha256: `sha256:${"a".repeat(64)}`,
    }));
    const manifest = {
      schemaVersion: 1,
      kind: "cloud-agents-platform-release",
      version: "0.1.0",
      sourceCommit: "a".repeat(40),
      sourceDirty: false,
      artifacts,
    };
    expect(() => validatePlatformReleaseManifest(manifest)).not.toThrow();
    expect(() =>
      validatePlatformReleaseManifest({ ...manifest, artifacts: artifacts.slice(1) }),
    ).toThrow(/artifacts/);
  });

  it("packages the current product migration manifest, catalog, and SQL", () => {
    const archive = buildPlatformMigrationPackage(process.cwd());
    const entries = readDeterministicUstar(new Uint8Array(archive));
    expect(entries.some(({ path }) => path.endsWith("product/000016/manifest.json"))).toBe(true);
    expect(entries.filter(({ path }) => path.endsWith(".sql")).length).toBe(16);
    expect(expectedArtifactIdentities()).toContainEqual({
      name: "cloud-agents-migrations",
      target: "portable",
    });
  });

  it("packages the independent Compose deployment inputs", () => {
    const archive = buildPlatformDeploymentPackage(process.cwd());
    const entries = readDeterministicUstar(new Uint8Array(archive));
    expect(entries.map(({ path }) => path)).toEqual([
      "deploy/bootstrap/database.sql",
      "deploy/bootstrap/roles.sql",
      "deploy/compose/.env.example",
      "deploy/compose/README.md",
      "deploy/compose/docker-compose.yml",
      "deploy/docker/control-plane.Dockerfile",
      "deploy/docker/migrate.Dockerfile",
      "deploy/docker/worker.Dockerfile",
    ]);
  });
});
