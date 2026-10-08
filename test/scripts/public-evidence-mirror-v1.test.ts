import {
  cpSync,
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
  assertPublicEvidenceMirrorV1Current,
  buildPublicEvidenceMirrorV1,
  PUBLIC_EVIDENCE_MIRROR_V1_MANIFEST_PATH,
  PUBLIC_EVIDENCE_MIRROR_V1_SCHEMA_PATH,
  sanitizePublicEvidenceText,
  writePublicEvidenceMirrorV1,
} from "../../scripts/lib/public-evidence-mirror-v1";

const root = new URL("../..", import.meta.url).pathname;

describe("public evidence mirror v1", () => {
  it("replaces only the declared local-boundary fields", () => {
    const result = sanitizePublicEvidenceText(
      "host=/Users/huang/devel/project/file.log tmp=/tmp/run-1.log " +
        "privateTmp=/private/tmp/run-2.log bareTmp=/tmp barePrivateTmp=/private/tmp " +
        "placeholder=/private<evidence-artifact> tmpPlaceholder=/tmp<evidence-artifact> " +
        "vm=192.168.139.215 pod=10.42.0.57 podCidr=10.42.0.0/24 " +
        "shortPodCidr=10.42/24 service=10.43.0.0/16 serviceIp=10.43.0.1 shortServiceCidr=10.43/16 " +
        "summary=10.42/10.43 " +
        "privateA=192.168.31.1 privateB=172.20.4.9 privateC=10.99.0.3 " +
        "loopback=127.0.0.1:8080 " +
        "kubernetes=https://k8s.orb.local:26443 " +
        "credentials=provider-credentials/*.json/provider-credentials/capability/",
    );

    expect(result.text).toBe(
      "host=<host-path> tmp=<evidence-artifact> " +
        "privateTmp=<evidence-artifact> bareTmp=<evidence-artifact> barePrivateTmp=<evidence-artifact> " +
        "placeholder=<evidence-artifact> tmpPlaceholder=<evidence-artifact> " +
        "vm=<test-vm-ip> pod=<pod-ip> podCidr=<pod-cidr> " +
        "shortPodCidr=<pod-cidr> service=<service-cidr> serviceIp=<service-ip> shortServiceCidr=<service-cidr> " +
        "summary=<cluster-cidr-summary> " +
        "privateA=<private-ip> privateB=<private-ip> privateC=<private-ip> " +
        "loopback=<loopback-endpoint> " +
        "kubernetes=<kubernetes-api-endpoint> " +
        "credentials=<credential-path>",
    );
    expect(result.replacements).toEqual({
      "host-path": 1,
      "tmp-artifact": 6,
      "pod-cidr": 2,
      "service-cidr": 2,
      "cluster-cidr-summary": 1,
      "pod-ip": 1,
      "service-ip": 1,
      "vm-ip": 1,
      "private-ip": 3,
      "loopback-endpoint": 1,
      "kubernetes-api-endpoint": 1,
      "credential-path": 1,
      "provider-token-env": 0,
    });
  });

  it("rejects a credential-shaped value before it can enter the mirror", () => {
    expect(() =>
      sanitizePublicEvidenceText(
        "provider-credentials/api.json token=sk-test-" + "12345678901234567890",
      ),
    ).toThrowError(
      new Error("Credential-shaped value found in public evidence source; value withheld."),
    );
  });

  it("builds a sanitized mirror without leaking current local boundary values", () => {
    const document = buildPublicEvidenceMirrorV1(root);
    expect(document.markdown).not.toMatch(
      /\/Users\/huang|\/private(?:<|\/)|(?:\/private)?\/tmp(?:\/|$)|provider-credentials/,
    );
    expect(document.markdown).not.toMatch(
      /192\.168\.139\.215|10\.42\.0\.|10\.43\.0\.|127\.0\.0\.1|k8s\.orb\.local/,
    );
    expect(document.markdown).not.toMatch(
      /\b(?:10\.(?:[0-9]{1,3}\.){2}[0-9]{1,3}|172\.(?:1[6-9]|2[0-9]|3[0-1])\.(?:[0-9]{1,3}\.){1,2}[0-9]{1,3}|192\.168\.(?:[0-9]{1,3}\.)[0-9]{1,3})\b/,
    );
    expect(document.markdown).not.toMatch(/ANTHROPIC_API_KEY|ANTHROPIC_AUTH_TOKEN/);
    expect(document.markdown).not.toMatch(/\]\([^)]*(?:phase-1|06-status-tracker)\.md/);
    expect(document.markdown).toContain(
      "](../plan/cloud-agents-platform/04-extraction-and-migration.md#",
    );
    expect(document.markdown).toContain("](../plan/cloud-agents-platform/evidence/README.md)");
    expect(document.markdown).toContain(
      "](../plan/cloud-agents-platform/templates/gate-closure-record.md)",
    );
    expect(document.markdown).toContain(
      "](../plan/cloud-agents-platform/migration-and-rollback-safety.md)",
    );
    expect(document.manifest.sanitization.publicLinksNormalized).toBeGreaterThan(0);
    expect(document.manifest.sourceInputs).toHaveLength(2);
    expect(document.manifest.output.path).toBe("docs/acceptance/phase-1.public.md");
    expect(document.manifest.omittedHistoricalObjects.count).toBe(138);
  });

  it("writes deterministic outputs and fails closed when a source drifts", () => {
    const fixture = mkdtempSync(join(tmpdir(), "public-evidence-mirror-v1-test-"));
    try {
      const phasePath = join(fixture, "docs/acceptance/phase-1.md");
      const statusPath = join(fixture, "docs/plan/cloud-agents-platform/06-status-tracker.md");
      mkdirSync(join(fixture, "docs/acceptance"), { recursive: true });
      mkdirSync(join(fixture, "docs/plan/cloud-agents-platform"), { recursive: true });
      writeFileSync(phasePath, "# Phase\n\n/tmp/run.log provider-credentials/*.json\n");
      writeFileSync(statusPath, "# Status\n\n192.168.139.215 10.42.0.0/24\n");
      cpSync(
        join(root, PUBLIC_EVIDENCE_MIRROR_V1_SCHEMA_PATH),
        join(fixture, PUBLIC_EVIDENCE_MIRROR_V1_SCHEMA_PATH),
      );

      writePublicEvidenceMirrorV1(fixture);
      const first = readFileSync(join(fixture, "docs/acceptance/phase-1.public.md"), "utf8");
      writePublicEvidenceMirrorV1(fixture);
      expect(readFileSync(join(fixture, "docs/acceptance/phase-1.public.md"), "utf8")).toBe(first);
      expect(() => assertPublicEvidenceMirrorV1Current(fixture)).not.toThrow();

      const manifestPath = join(fixture, PUBLIC_EVIDENCE_MIRROR_V1_MANIFEST_PATH);
      const malformedManifest = JSON.parse(readFileSync(manifestPath, "utf8")) as {
        sourceInputs: Array<{ path: string }>;
      };
      malformedManifest.sourceInputs[0].path = "../secret";
      writeFileSync(manifestPath, `${JSON.stringify(malformedManifest)}\n`);
      expect(() => assertPublicEvidenceMirrorV1Current(fixture)).toThrow(/schema validation/i);
      writePublicEvidenceMirrorV1(fixture);

      const duplicateRuleManifest = JSON.parse(readFileSync(manifestPath, "utf8")) as {
        sanitization: { replacementRules: Array<{ id: string }> };
      };
      duplicateRuleManifest.sanitization.replacementRules[1].id = "host-path";
      writeFileSync(manifestPath, `${JSON.stringify(duplicateRuleManifest)}\n`);
      expect(() => assertPublicEvidenceMirrorV1Current(fixture)).toThrow(/schema validation/i);
      writePublicEvidenceMirrorV1(fixture);

      writeFileSync(phasePath, "# Phase\n\nsource drift\n");
      expect(() => assertPublicEvidenceMirrorV1Current(fixture)).toThrow(/drifted/i);
    } finally {
      rmSync(fixture, { recursive: true, force: true });
    }
  });

  it("rejects symlinked output files before writing outside the repository", () => {
    const fixture = mkdtempSync(join(tmpdir(), "public-evidence-mirror-v1-symlink-test-"));
    const outside = `${fixture}-sentinel.md`;
    try {
      mkdirSync(join(fixture, "docs/acceptance"), { recursive: true });
      mkdirSync(join(fixture, "docs/plan/cloud-agents-platform"), { recursive: true });
      writeFileSync(join(fixture, "docs/acceptance/phase-1.md"), "# Phase\n");
      writeFileSync(
        join(fixture, "docs/plan/cloud-agents-platform/06-status-tracker.md"),
        "# Status\n",
      );
      cpSync(
        join(root, PUBLIC_EVIDENCE_MIRROR_V1_SCHEMA_PATH),
        join(fixture, PUBLIC_EVIDENCE_MIRROR_V1_SCHEMA_PATH),
      );
      writeFileSync(outside, "SENTINEL\n");
      symlinkSync(outside, join(fixture, "docs/acceptance/phase-1.public.md"));

      expect(() => writePublicEvidenceMirrorV1(fixture)).toThrow(/non-symlink output file/i);
      expect(readFileSync(outside, "utf8")).toBe("SENTINEL\n");
    } finally {
      rmSync(fixture, { recursive: true, force: true });
      rmSync(outside, { force: true });
    }
  });
});
