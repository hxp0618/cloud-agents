import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import {
  type CloudAgentCommandEnvelope,
  type CloudAgentMessageEnvelope,
  validateCloudAgentCommandEnvelope,
  validateCloudAgentMessageEnvelope,
} from "@cloud-agents/cloud-agent-protocol";

import { assertCloudAgentTranscript } from "./index";

type TranscriptCase = {
  readonly id: string;
  readonly expected: "ACCEPT" | "REJECT";
  readonly errorIncludes?: string;
  readonly coverage: ReadonlyArray<string>;
  readonly commands: ReadonlyArray<CloudAgentCommandEnvelope>;
  readonly messages: ReadonlyArray<CloudAgentMessageEnvelope>;
};

const fixtureRoot = new URL("../../cloud-agent-protocol/fixtures/p0/", import.meta.url);
const manifest = readJson<{
  readonly requiredCoverage: { readonly transcriptSemantics: ReadonlyArray<string> };
}>("corpus-manifest.json");
const transcripts = readJson<{ readonly cases: ReadonlyArray<TranscriptCase> }>("transcripts.json");

describe("P0 golden transcript semantics", () => {
  it("checks correlation, generation, ordering, terminal, late-frame, and Stop outcomes with the public testkit", () => {
    const observedCoverage = new Set<string>();
    for (const fixture of transcripts.cases) {
      for (const frame of fixture.commands) {
        expect(validateCloudAgentCommandEnvelope(frame).valid, fixture.id).toBe(true);
      }
      for (const frame of fixture.messages) {
        expect(validateCloudAgentMessageEnvelope(frame).valid, fixture.id).toBe(true);
      }
      for (const coverage of fixture.coverage) observedCoverage.add(coverage);
      if (fixture.expected === "ACCEPT") {
        expect(
          () => assertCloudAgentTranscript(fixture.commands, fixture.messages),
          fixture.id,
        ).not.toThrow();
      } else {
        expect(
          () => assertCloudAgentTranscript(fixture.commands, fixture.messages),
          fixture.id,
        ).toThrow(fixture.errorIncludes);
      }
    }
    expect([...observedCoverage].toSorted()).toEqual(
      [...manifest.requiredCoverage.transcriptSemantics].toSorted(),
    );
  });
});

function readJson<T>(path: string): T {
  return JSON.parse(readFileSync(new URL(path, fixtureRoot), "utf8")) as T;
}
