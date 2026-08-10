import { readFileSync } from "node:fs";

import Ajv2020 from "ajv/dist/2020.js";
import addFormats from "ajv-formats";
import { describe, expect, it } from "vitest";

import envelopeSchema from "../schemas/cloud-agent-envelope-v2.schema.json";

import {
  CLOUD_AGENT_COMMAND_TYPES,
  type CloudAgentCommandEnvelope,
  type CloudAgentMessageEnvelope,
  validateCloudAgentCommandEnvelope,
  validateCloudAgentMessageEnvelope,
} from "./index";

type FrameCase = {
  readonly id: string;
  readonly kind: "command" | "message";
  readonly commandType?: string;
  readonly messageType?: string;
  readonly category?: string;
  readonly expect: { readonly parser: boolean; readonly schema: boolean };
  readonly frame: unknown;
};

type TranscriptCase = {
  readonly id: string;
  readonly expected: "ACCEPT" | "REJECT";
  readonly coverage: ReadonlyArray<string>;
  readonly commands: ReadonlyArray<CloudAgentCommandEnvelope>;
  readonly messages: ReadonlyArray<CloudAgentMessageEnvelope>;
};

const fixtureRoot = new URL("../fixtures/p0/", import.meta.url);
const manifest = readJson<{
  readonly corpusVersion: string;
  readonly requiredCoverage: {
    readonly commandsV22: ReadonlyArray<string>;
    readonly commandsV23: ReadonlyArray<string>;
    readonly messageVariants: ReadonlyArray<string>;
    readonly negativeCategories: ReadonlyArray<string>;
    readonly correlationFields: ReadonlyArray<string>;
    readonly transcriptSemantics: ReadonlyArray<string>;
    readonly characterizationOnly: ReadonlyArray<string>;
    readonly referenceHostLifecycle: ReadonlyArray<string>;
  };
}>("corpus-manifest.json");
const v22Commands = readJsonLines("v2.2/commands.jsonl");
const v22Messages = readJsonLines("v2.2/messages.jsonl");
const v23Commands = readJsonLines("v2.3/commands.jsonl");
const v23Messages = readJsonLines("v2.3/messages.jsonl");
const negativeFrames = readJsonLines("negative/frames.jsonl");
const transcripts = readJson<{ readonly cases: ReadonlyArray<TranscriptCase> }>("transcripts.json");
const semanticOracles = readJson<{
  readonly cases: ReadonlyArray<{
    readonly coverage: string;
    readonly status: "NOT_ENFORCED" | "NOT_RUN";
  }>;
}>("semantic-oracles.json");
const referenceHostLifecycle = readJson<{
  readonly schemaVersion: string;
  readonly oracleType: string;
  readonly productionExecution: "NOT_RUN";
  readonly cases: ReadonlyArray<{
    readonly id: string;
    readonly coverage: ReadonlyArray<string>;
    readonly trace?: ReadonlyArray<{ readonly sequence: number }>;
    readonly resourceLifecycles?: ReadonlyArray<{
      readonly resourceKind: string;
      readonly lifecycleId: string;
      readonly states: ReadonlyArray<string>;
    }>;
    readonly durableReceipt?: Readonly<Record<string, unknown>>;
    readonly forbiddenDurableKeys?: ReadonlyArray<string>;
  }>;
}>("reference-host-lifecycle-v1.json");

const ajv = new Ajv2020({ allErrors: true, strict: true });
addFormats(ajv);
const validateSchema = ajv.compile(envelopeSchema);

describe("P0 Protocol 2.2/2.3 golden corpus", () => {
  it("is versioned and covers every 2.2/2.3 command and message variant", () => {
    expect(manifest.corpusVersion).toBe("p0-protocol-golden-v3");
    expect(values(v22Commands, "commandType")).toEqual(
      sorted(manifest.requiredCoverage.commandsV22),
    );
    expect(values(v23Commands, "commandType")).toEqual(
      sorted(manifest.requiredCoverage.commandsV23),
    );
    expect(values(v22Messages, "messageType")).toEqual(
      sorted(manifest.requiredCoverage.messageVariants),
    );
    expect(values(v23Messages, "messageType")).toEqual(
      sorted(manifest.requiredCoverage.messageVariants),
    );
    expect(manifest.requiredCoverage.commandsV23).toEqual([...CLOUD_AGENT_COMMAND_TYPES]);
    expect(manifest.requiredCoverage.commandsV22).toEqual(
      CLOUD_AGENT_COMMAND_TYPES.filter((command) => command !== "GenerateText"),
    );
  });

  it("characterizes every golden frame with the public structural parser and JSON Schema", () => {
    for (const fixture of [
      ...v22Commands,
      ...v22Messages,
      ...v23Commands,
      ...v23Messages,
      ...negativeFrames,
    ]) {
      const parserResult =
        fixture.kind === "command"
          ? validateCloudAgentCommandEnvelope(fixture.frame)
          : validateCloudAgentMessageEnvelope(fixture.frame);
      expect(parserResult.valid, `${fixture.id}: ${JSON.stringify(parserResult)}`).toBe(
        fixture.expect.parser,
      );
      expect(
        validateSchema(fixture.frame),
        `${fixture.id}: ${JSON.stringify(validateSchema.errors)}`,
      ).toBe(fixture.expect.schema);
    }
  });

  it("covers the declared negative categories without hiding parser/schema disagreement", () => {
    expect([...new Set(values(negativeFrames, "category"))]).toEqual(
      sorted(manifest.requiredCoverage.negativeCategories),
    );
    expect(
      negativeFrames.filter((fixture) => fixture.expect.parser !== fixture.expect.schema).length,
    ).toBeGreaterThanOrEqual(3);
  });

  it("rejects malformed NDJSON or structurally invalid JSON values", () => {
    const lines = readText("negative/invalid-ndjson.txt").trim().split("\n");
    let malformed = 0;
    let invalidEnvelope = 0;
    for (const line of lines) {
      try {
        const value: unknown = JSON.parse(line);
        expect(validateCloudAgentCommandEnvelope(value).valid).toBe(false);
        expect(validateCloudAgentMessageEnvelope(value).valid).toBe(false);
        invalidEnvelope += 1;
      } catch {
        malformed += 1;
      }
    }
    expect({ malformed, invalidEnvelope }).toEqual({ malformed: 2, invalidEnvelope: 2 });
  });

  it("keeps every transcript frame structurally parseable and semantic coverage complete", () => {
    const observedCoverage = new Set<string>();
    for (const fixture of transcripts.cases) {
      for (const frame of fixture.commands) {
        expect(validateCloudAgentCommandEnvelope(frame).valid, fixture.id).toBe(true);
        expect(validateSchema(frame), fixture.id).toBe(frame.protocolVersion.minor >= 3);
      }
      for (const frame of fixture.messages) {
        expect(validateCloudAgentMessageEnvelope(frame).valid, fixture.id).toBe(true);
        expect(validateSchema(frame), fixture.id).toBe(frame.protocolVersion.minor >= 3);
      }
      for (const coverage of fixture.coverage) observedCoverage.add(coverage);
    }
    expect([...observedCoverage].toSorted()).toEqual(
      [...manifest.requiredCoverage.transcriptSemantics].toSorted(),
    );
  });

  it("includes a positive Error terminal and a negative for every correlation field", () => {
    expect(
      transcripts.cases.some(
        (fixture) =>
          fixture.expected === "ACCEPT" &&
          fixture.coverage.includes("error-terminal") &&
          fixture.messages.some((message) => message.messageType === "Error"),
      ),
    ).toBe(true);
    expect(manifest.requiredCoverage.correlationFields).toEqual([
      "requestId",
      "executionId",
      "generation",
      "commandId",
    ]);
    for (const field of manifest.requiredCoverage.correlationFields) {
      expect(
        transcripts.cases.some(
          (fixture) =>
            fixture.expected === "REJECT" &&
            fixture.coverage.includes(`correlation-${correlationCoverageName(field)}`),
        ),
        field,
      ).toBe(true);
    }
  });

  it("keeps capability and authenticated Provider gaps explicitly non-closed", () => {
    expect(
      [...new Set(semanticOracles.cases.map((fixture) => fixture.coverage))].toSorted(),
    ).toEqual([...manifest.requiredCoverage.characterizationOnly].toSorted());
    expect(semanticOracles.cases.every((fixture) => fixture.status !== undefined)).toBe(true);
    expect(semanticOracles.cases.some((fixture) => fixture.status === "NOT_RUN")).toBe(true);
    expect(semanticOracles.cases.some((fixture) => fixture.status === "NOT_ENFORCED")).toBe(true);
  });

  it("keeps the reference-host lifecycle as a complete greenfield spec oracle", () => {
    expect(referenceHostLifecycle.schemaVersion).toBe(
      "cloud-agents.reference-host-lifecycle-trace/v1",
    );
    expect(referenceHostLifecycle.oracleType).toBe("GREENFIELD_SPEC_ORACLE");
    expect(referenceHostLifecycle.productionExecution).toBe("NOT_RUN");
    expect(
      [...new Set(referenceHostLifecycle.cases.flatMap((fixture) => fixture.coverage))].toSorted(),
    ).toEqual([...manifest.requiredCoverage.referenceHostLifecycle].toSorted());
    for (const fixture of referenceHostLifecycle.cases) {
      if (fixture.trace) {
        expect(
          fixture.trace.map((event) => event.sequence),
          fixture.id,
        ).toEqual(fixture.trace.map((_, index) => index + 1));
      }
      if (fixture.durableReceipt && fixture.forbiddenDurableKeys) {
        for (const forbidden of fixture.forbiddenDurableKeys) {
          expect(fixture.durableReceipt, `${fixture.id}: ${forbidden}`).not.toHaveProperty(
            forbidden,
          );
        }
      }
      if (fixture.resourceLifecycles) {
        expect(
          fixture.resourceLifecycles.map((resource) => resource.resourceKind).toSorted(),
          fixture.id,
        ).toEqual(["endpoint", "grant", "volume", "workload"]);
        expect(
          new Set(fixture.resourceLifecycles.map((resource) => resource.lifecycleId)).size,
          fixture.id,
        ).toBe(fixture.resourceLifecycles.length);
        for (const resource of fixture.resourceLifecycles) {
          expect(resource.states.at(-1), `${fixture.id}: ${resource.resourceKind}`).toBe("deleted");
        }
      }
    }
    const byId = new Map(referenceHostLifecycle.cases.map((fixture) => [fixture.id, fixture]));
    expect(byId.get("controller-restart-replays-durable-receipts")?.trace).toHaveLength(5);
    expect(byId.get("partial-allocation-reconciles-orphans")?.trace).toHaveLength(7);
    expect(byId.get("resources-have-independent-lifecycles")?.resourceLifecycles).toHaveLength(4);
  });
});

function readJson<T>(path: string): T {
  return JSON.parse(readText(path)) as T;
}

function readJsonLines(path: string): ReadonlyArray<FrameCase> {
  return readText(path)
    .trim()
    .split("\n")
    .map((line) => JSON.parse(line) as FrameCase);
}

function readText(path: string): string {
  return readFileSync(new URL(path, fixtureRoot), "utf8");
}

function values(
  fixtures: ReadonlyArray<FrameCase>,
  key: "commandType" | "messageType" | "category",
) {
  return fixtures.map((fixture) => String(fixture[key])).toSorted();
}

function sorted(values: ReadonlyArray<string>) {
  return [...values].toSorted();
}

function correlationCoverageName(field: string): string {
  return field.replace(/[A-Z]/gu, (letter) => `-${letter.toLowerCase()}`);
}
