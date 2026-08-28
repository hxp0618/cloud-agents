import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";
import { describe, expect, it } from "vitest";

import {
  assertManagedAgentLocalExecutionCurrent,
  buildManagedAgentLocalExecutionProfile,
  buildManagedAgentLocalExecutionProfileSchema,
  buildManagedAgentLocalExecutionSource,
  buildManagedAgentLocalExecutionSourceSchema,
  MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH,
  MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH,
  managedAgentLocalExecutionProfileDigest,
  managedAgentLocalExecutionSourceDigest,
} from "./managed-agent-local-execution-profile";

const root = resolve(import.meta.dirname, "../..");

describe("D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1 generated authority", () => {
  it("is deterministic, strict, and current", () => {
    const source = buildManagedAgentLocalExecutionSource();
    const profile = buildManagedAgentLocalExecutionProfile();
    expect(source.authorityId).toBe("D-055-MANAGED-AGENT-WORKER-COORDINATION-000001");
    expect(source.revision).toBe("D-055-MANAGED-AGENT-WORKER-COORDINATION-000001.r1");
    expect(profile.profileId).toBe(
      "cloud-agents/managed-agent-worker-local-execution/localdev-v1alpha1",
    );
    expect(source.sourceDigest).toBe(managedAgentLocalExecutionSourceDigest());
    expect(profile.profileDigest).toBe(managedAgentLocalExecutionProfileDigest());
    expect(profile.parentProfiles).toEqual([
      "cloud-agents/managed-agent-lifecycle/v1alpha1",
      "cloud-agents/managed-agent-events/v1alpha1",
      "cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1",
      "cloud-agents/worker-operation-admission/v1alpha1",
      "cloud-agents/worker-operation-execution/localdev-v1alpha1",
    ]);
    expect(profile.commands).toEqual(["Probe", "ValidateBinding"]);
    expect(profile.inputPaths).toHaveLength(43);
    expect(profile.exclusionPaths).toHaveLength(13);
    expect(profile.inputManifestDigest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    expect(profile.scopeProjection).toBe("sha256-length-prefixed-tenant-project-v1");
    expect((profile.archive as Record<string, unknown>).emission).toBe("forbidden");
    expect((profile.memberManifest as Record<string, unknown>).emission).toBe("forbidden");
    expect((profile.receipt as Record<string, unknown>).persistence).toBe("no_write");
    expect((profile.receipt as Record<string, unknown>).resultDigestAlgorithm).toBe(
      "sha256:deterministic-protobuf-receipt-result-v1",
    );
    expect(profile.externalSideEffects).toEqual({
      database: false,
      durableReceipt: false,
      http: false,
      p2: false,
      provider: false,
      workspace: false,
      artifact: false,
      credential: false,
      deployment: false,
      publication: false,
      gate: false,
    });
    assertManagedAgentLocalExecutionCurrent(root);
  });

  it("rejects identity, input-set, archive, receipt, and side-effect drift", () => {
    const sourceSchema = JSON.parse(
      readFileSync(resolve(root, MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH), "utf8"),
    ) as Record<string, unknown>;
    const profileSchema = JSON.parse(
      readFileSync(resolve(root, MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH), "utf8"),
    ) as Record<string, unknown>;
    const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: false });
    const validateSource = ajv.compile(sourceSchema);
    const validateProfile = ajv.compile(profileSchema);
    expect(validateSource(buildManagedAgentLocalExecutionSource())).toBe(true);
    expect(validateProfile(buildManagedAgentLocalExecutionProfile())).toBe(true);

    for (const mutate of [
      (value: Record<string, any>) => {
        value.profileId = "caller-selected";
      },
      (value: Record<string, any>) => {
        value.inputPaths = [...value.inputPaths, "untracked.txt"];
      },
      (value: Record<string, any>) => {
        value.inputManifestDigest = "sha256:" + "0".repeat(64);
      },
      (value: Record<string, any>) => {
        value.scopeProjection = "caller-selected";
      },
      (value: Record<string, any>) => {
        value.exclusionPaths = value.exclusionPaths.filter((path: string) => path !== ".idea");
      },
      (value: Record<string, any>) => {
        value.archive.emission = "write";
      },
      (value: Record<string, any>) => {
        value.receipt.persistence = "postgres";
      },
      (value: Record<string, any>) => {
        value.externalSideEffects.database = true;
      },
      (value: Record<string, any>) => {
        value.unexpected = true;
      },
    ]) {
      const source = JSON.parse(JSON.stringify(buildManagedAgentLocalExecutionSource()));
      const profile = JSON.parse(JSON.stringify(buildManagedAgentLocalExecutionProfile()));
      mutate(source);
      mutate(profile);
      expect(validateSource(source)).toBe(false);
      expect(validateProfile(profile)).toBe(false);
    }
  });

  it("keeps checked-in generated bytes equal to the authority", () => {
    expect(
      JSON.parse(readFileSync(resolve(root, MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_PATH), "utf8")),
    ).toEqual(buildManagedAgentLocalExecutionSource());
    expect(
      JSON.parse(readFileSync(resolve(root, MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_PATH), "utf8")),
    ).toEqual(buildManagedAgentLocalExecutionProfile());
    expect(
      JSON.parse(
        readFileSync(resolve(root, MANAGED_AGENT_LOCAL_EXECUTION_SOURCE_SCHEMA_PATH), "utf8"),
      ),
    ).toEqual(buildManagedAgentLocalExecutionSourceSchema());
    expect(
      JSON.parse(
        readFileSync(resolve(root, MANAGED_AGENT_LOCAL_EXECUTION_PROFILE_SCHEMA_PATH), "utf8"),
      ),
    ).toEqual(buildManagedAgentLocalExecutionProfileSchema());
  });
});
