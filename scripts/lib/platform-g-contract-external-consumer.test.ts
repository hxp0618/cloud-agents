import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  buildExternalConsumerProfile,
  readExternalConsumerSource,
  type ExternalConsumerEvidence,
} from "./platform-g-contract-external-consumer";

const root = resolve(import.meta.dirname, "../..");
const harnessPath = "scripts/test-platform-sdk-consumers.ts";
const harnessBytes = readFileSync(resolve(root, harnessPath));
const harness = {
  path: harnessPath,
  sha256: `sha256:${createHash("sha256").update(harnessBytes).digest("hex")}`,
  sizeBytes: harnessBytes.byteLength,
  mode: "100644" as const,
};

function evidence(): ExternalConsumerEvidence {
  return {
    formatVersion: "cloud-agents-platform-sdk-consumer-evidence/v1",
    harness,
    toolchain: {
      bun: "1.4.0",
      go: "go version go1.27.0 darwin/arm64",
      typescript: "5.7.3",
      goFlags: "-mod=readonly",
      goWork: "off",
    },
    typescript: {
      package: "@synara/cloud-agent-platform-sdk",
      version: "0.0.0-a3.2",
      toolchain: "bun@1.4.0;typescript@5.7.3",
      artifactPath: "typescript-pack/synara-cloud-agent-platform-sdk-0.0.0-a3.2.tgz",
      artifactSha256: `sha256:${"1".repeat(64)}`,
      integrity: `sha512-${"A".repeat(86)}==`,
      lockArtifactUrl:
        "http://127.0.0.1:<ephemeral-port>/typescript-pack/synara-cloud-agent-platform-sdk-0.0.0-a3.2.tgz",
      lockIntegrity: `sha512-${"A".repeat(86)}==`,
      dependencies: { "@bufbuild/protobuf": "2.14.0", "@connectrpc/connect": "2.1.2" },
      dependencyArtifacts: {
        "@bufbuild/protobuf": {
          version: "2.14.0",
          artifactPath: "npm-registry-tar/@bufbuild/protobuf/bufbuild-protobuf-2.14.0.tgz",
          sha256: `sha256:${"5".repeat(64)}`,
          integrity: `sha512-${"B".repeat(86)}==`,
        },
        "@connectrpc/connect": {
          version: "2.1.2",
          artifactPath: "npm-registry-tar/@connectrpc/connect/connectrpc-connect-2.1.2.tgz",
          sha256: `sha256:${"6".repeat(64)}`,
          integrity: `sha512-${"C".repeat(86)}==`,
        },
        "@types/node": {
          version: "24.10.13",
          artifactPath: "npm-registry-tar/@types/node/types-node-24.10.13.tgz",
          sha256: `sha256:${"7".repeat(64)}`,
          integrity: `sha512-${"D".repeat(86)}==`,
        },
        "undici-types": {
          version: "7.16.0",
          artifactPath: "npm-registry-tar/undici-types/undici-types-7.16.0.tgz",
          sha256: `sha256:${"8".repeat(64)}`,
          integrity: `sha512-${"E".repeat(86)}==`,
        },
        typescript: {
          version: "5.7.3",
          artifactPath: "npm-registry-tar/typescript/typescript-5.7.3.tgz",
          sha256: `sha256:${"9".repeat(64)}`,
          integrity: `sha512-${"F".repeat(86)}==`,
        },
      },
      fixture: {
        transport: "connect",
        method: "POST",
        path: "/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate",
        requestContentType: "application/proto",
        responseContentType: "application/proto",
        loopback: true,
        callCount: 1,
      },
      loopbackCallCount: 1,
    },
    go: {
      module: "github.com/hxp0618/cloud-agents/sdk/go",
      version: "v0.0.0-a3.2",
      toolchain: "go1.27.0",
      moduleProxyPath: "go-proxy/github.com/hxp0618/cloud-agents/sdk/go/@v/v0.0.0-a3.2.zip",
      moduleZipSha256: `sha256:${"2".repeat(64)}`,
      goModSha256: `sha256:${"3".repeat(64)}`,
      goSumSha256: `sha256:${"4".repeat(64)}`,
      moduleSum: `h1:${"A".repeat(43)}=`,
      goModSum: `h1:${"B".repeat(43)}=`,
      goFlags: "-mod=readonly",
      goWork: "off",
      goproxy: "http://127.0.0.1:<ephemeral-port>/go-proxy",
      fixture: {
        transport: "connect",
        method: "POST",
        path: "/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate",
        requestContentType: "application/proto",
        responseContentType: "application/proto",
        loopback: true,
        callCount: 1,
      },
      loopbackCallCount: 1,
    },
  };
}

describe("versioned G-CONTRACT external-consumer successor", () => {
  it("validates source and emits deterministic, non-Gate profile", () => {
    const source = readExternalConsumerSource(root);
    expect(source.predecessorFence.mutation).toBe("forbidden");
    const first = buildExternalConsumerProfile(root, evidence());
    const second = buildExternalConsumerProfile(root, evidence());
    expect(first).toEqual(second);
    expect(first.implementationBoundary).toMatchObject({
      notGateClosure: true,
      gateStatus: "ALL_GATES_OPEN",
      loopbackFixtureAllowed: true,
    });
    expect(first.harness).toEqual(harness);
    expect((first.consumers as Record<string, any>).typescript.loopbackCallCount).toBe(1);
    expect((first.consumers as Record<string, any>).go.loopbackCallCount).toBe(1);
  });

  it.each([
    [
      "call-count",
      (value: ExternalConsumerEvidence) => {
        (value.go as any).loopbackCallCount = 2;
      },
    ],
    [
      "digest",
      (value: ExternalConsumerEvidence) => {
        (value.go as any).goSumSha256 = "missing";
      },
    ],
    [
      "local-dependency",
      (value: ExternalConsumerEvidence) => {
        (value.typescript.dependencies as any).source = "file:../sdk";
      },
    ],
    [
      "unknown-field",
      (value: ExternalConsumerEvidence) => {
        (value as any).unexpected = "must be rejected";
      },
    ],
    [
      "external-url",
      (value: ExternalConsumerEvidence) => {
        (value.go as any).goproxy = "https://proxy.golang.org";
      },
    ],
    [
      "content-type",
      (value: ExternalConsumerEvidence) => {
        (value.typescript.fixture as any).responseContentType = "application/json";
      },
    ],
    [
      "checksum-shape",
      (value: ExternalConsumerEvidence) => {
        (value.go as any).moduleSum = "h1:short";
      },
    ],
  ])("fails closed on %s evidence", (_name, mutate) => {
    const value = evidence();
    mutate(value);
    expect(() => buildExternalConsumerProfile(root, value)).toThrow();
  });
});
