import { mkdirSync, mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { canonicalJsonDigest } from "../../scripts/lib/platform-json-semantics";
import {
  buildExternalConsumerV3ConsumerBindings,
  runExternalConsumerV3NativeReplay,
} from "../../scripts/lib/platform-g-contract-external-consumer-v3-native";

const root = new URL("../..", import.meta.url).pathname;
const nativePlatform = process.platform === "darwin" ? "darwin-arm64" : "linux-amd64";

const projectPath = "/v1/tenants/tenant-alpha/projects/project-alpha";
const negotiatePath = "/cloudagents.worker.v1alpha1.WorkerExecutionService/Negotiate";
const result = { project: { metadata: { uid: "project-alpha" } }, negotiationProtoHex: "" };
const requests = (consumer: string) => [
  { consumer, method: "GET", path: projectPath, contentType: null, status: 200 },
  { consumer, method: "POST", path: negotiatePath, contentType: "application/proto", status: 200 },
];
const outcome = (consumer: string) => ({ kind: "outcome", consumer, result });
const records = () => [
  ...requests("typescript"),
  outcome("typescript"),
  ...requests("go"),
  outcome("go"),
];
const jsonl = (values: unknown[]) => values.map((value) => JSON.stringify(value)).join("\n");

describe("G-CONTRACT observed consumer evidence", () => {
  it("binds actual decoded results and counts each consumer independently", () => {
    const observed = buildExternalConsumerV3ConsumerBindings(
      jsonl([
        {
          consumer: "unknown",
          method: "GET",
          path: "/go-proxy/sdk.zip",
          contentType: null,
          status: 200,
        },
        ...records(),
      ]),
    );
    expect(observed.typescript.observedCallCount).toBe(1);
    expect(observed.go.observedCallCount).toBe(1);
    expect(observed.typescript.outputSha256).toBe(canonicalJsonDigest(result));
    expect(observed.go.outputSha256).toBe(canonicalJsonDigest(result));
    const changed = records();
    changed[5] = { ...outcome("go"), result: { ...result, negotiationProtoHex: "0801" } };
    expect(buildExternalConsumerV3ConsumerBindings(jsonl(changed)).go.outputSha256).not.toBe(
      observed.go.outputSha256,
    );
  });

  it("rejects identical totals when both calls belong to only one consumer", () => {
    expect(() =>
      buildExternalConsumerV3ConsumerBindings(
        jsonl([
          ...requests("typescript"),
          ...requests("typescript"),
          outcome("typescript"),
          outcome("go"),
        ]),
      ),
    ).toThrow(/exactly once.*typescript/);
  });

  it.each(["unknown", "python", ""])("rejects misattributed API requests (%s)", (consumer) => {
    expect(() =>
      buildExternalConsumerV3ConsumerBindings(
        jsonl([...requests(consumer), outcome("typescript"), ...requests("go"), outcome("go")]),
      ),
    ).toThrow(/consumer/);
  });

  it("rejects a missing or duplicate outcome instead of synthesizing a digest", () => {
    expect(() => buildExternalConsumerV3ConsumerBindings(jsonl(records().slice(0, -1)))).toThrow(
      /outcome.*go/,
    );
    expect(() =>
      buildExternalConsumerV3ConsumerBindings(jsonl([...records(), outcome("go")])),
    ).toThrow(/outcome.*go/);
  });

  it.each([
    null,
    {},
    { project: null, negotiationProtoHex: "" },
    { project: {}, negotiationProtoHex: "0g" },
  ])("rejects malformed decoded outcomes %j", (badResult) => {
    const values: unknown[] = records();
    values[2] = { kind: "outcome", consumer: "typescript", result: badResult };
    expect(() => buildExternalConsumerV3ConsumerBindings(jsonl(values))).toThrow(/outcome/);
  });

  it("rejects failed fixture requests, unknown record kinds and unlabeled requests", () => {
    for (const record of [
      { ...requests("go")[0], status: 401 },
      { kind: "unrecognized", consumer: "go" },
      { ...requests("go")[0], consumer: undefined },
    ]) {
      expect(() =>
        buildExternalConsumerV3ConsumerBindings(jsonl([...records(), record])),
      ).toThrow();
    }
  });
});

describe("G-CONTRACT external-consumer native replay", () => {
  it("rejects relative roots before reading projection or writing receipts", () => {
    expect(() =>
      runExternalConsumerV3NativeReplay({
        authorityRoot: root,
        projectionRoot: "relative-projection",
        outputRoot: root,
        platform: nativePlatform,
        runId: "A",
      }),
    ).toThrow("projection root must be absolute");
  });

  it("rejects roots nested under the authority tree", () => {
    mkdirSync(join(root, ".tmp"), { recursive: true });
    const fixture = mkdtempSync(join(root, ".tmp", "external-consumer-v3-native-test-"));
    try {
      const projection = join(fixture, "projection");
      const output = join(fixture, "output");
      mkdirSync(projection);
      mkdirSync(output);
      expect(() =>
        runExternalConsumerV3NativeReplay({
          authorityRoot: root,
          projectionRoot: projection,
          outputRoot: output,
          platform: nativePlatform,
          runId: "B",
        }),
      ).toThrow("must not be nested under the authority root");
    } finally {
      rmSync(fixture, { recursive: true, force: true });
    }
  });

  it("rejects a platform label that is not the current runtime", () => {
    expect(() =>
      runExternalConsumerV3NativeReplay({
        authorityRoot: root,
        projectionRoot: "/private/tmp/external-consumer-v3-native-projection",
        outputRoot: "/private/tmp/external-consumer-v3-native-output",
        platform: process.platform === "darwin" ? "linux-amd64" : "darwin-arm64",
        runId: "A",
      }),
    ).toThrow("does not match the current runtime");
  });
});
