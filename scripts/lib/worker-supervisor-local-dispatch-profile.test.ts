import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import Ajv2020 from "ajv/dist/2020.js";
import { describe, expect, it } from "vitest";

import {
  assertWorkerSupervisorLocalDispatchCurrent,
  buildWorkerSupervisorLocalDispatchProfile,
  buildWorkerSupervisorLocalDispatchProfileSchema,
  buildWorkerSupervisorLocalDispatchSource,
  buildWorkerSupervisorLocalDispatchSourceSchema,
  LOCAL_DISPATCH_PROFILE_PATH,
  LOCAL_DISPATCH_PROFILE_SCHEMA_PATH,
  LOCAL_DISPATCH_SOURCE_PATH,
  LOCAL_DISPATCH_SOURCE_SCHEMA_PATH,
  localDispatchProfileDigest,
  localDispatchSourceDigest,
} from "./worker-supervisor-local-dispatch-profile";

const root = resolve(import.meta.dirname, "../..");

describe("D-054-WORKER-DISPATCH-000001.r1 generated local dispatch profile", () => {
  it("is deterministic, strict, and current", () => {
    const source = buildWorkerSupervisorLocalDispatchSource();
    const profile = buildWorkerSupervisorLocalDispatchProfile();
    expect(source.authorityId).toBe("D-054-WORKER-DISPATCH-000001");
    expect(source.revision).toBe("D-054-WORKER-DISPATCH-000001.r1");
    expect(source.decision).toBe("D-054-WORKER-DISPATCH-000001.r1");
    expect(profile.profileId).toBe(
      "cloud-agents/worker-supervisor-operation-dispatch/localdev-v1alpha1",
    );
    expect((profile.reviewRules as Record<string, unknown>).reviewPath).toBe(
      "docs/plan/standalone/worker-supervisor-operation-dispatch-localdev-independent-review-20260828.md",
    );
    expect(source.sourceDigest).toBe(localDispatchSourceDigest());
    expect(profile.profileDigest).toBe(localDispatchProfileDigest());
    expect(profile.capabilities).toEqual(["negotiation", "health", "operation_dispatch"]);
    expect(profile.commands).toEqual(["Probe", "ValidateBinding"]);
    expect(profile.externalSideEffects).toEqual({
      database: false,
      durableReceipt: false,
      http: false,
      p2: false,
      provider: false,
      workspace: false,
      credential: false,
      artifact: false,
      deployment: false,
      publication: false,
    });
    assertWorkerSupervisorLocalDispatchCurrent(root);
  });

  it("rejects identity, capability, side-effect, and unknown-field mutations", () => {
    const sourceSchema = JSON.parse(
      readFileSync(resolve(root, LOCAL_DISPATCH_SOURCE_SCHEMA_PATH), "utf8"),
    ) as Record<string, unknown>;
    const profileSchema = JSON.parse(
      readFileSync(resolve(root, LOCAL_DISPATCH_PROFILE_SCHEMA_PATH), "utf8"),
    ) as Record<string, unknown>;
    const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: false });
    const validateSource = ajv.compile(sourceSchema);
    const validateProfile = ajv.compile(profileSchema);
    expect(validateSource(buildWorkerSupervisorLocalDispatchSource())).toBe(true);
    expect(validateProfile(buildWorkerSupervisorLocalDispatchProfile())).toBe(true);

    for (const mutate of [
      (value: Record<string, any>) => {
        value.profileId = "caller-selected";
      },
      (value: Record<string, any>) => {
        value.transport = "http";
      },
      (value: Record<string, any>) => {
        value.capabilities = ["negotiation", "health"];
      },
      (value: Record<string, any>) => {
        value.externalSideEffects.database = true;
      },
      (value: Record<string, any>) => {
        value.unexpected = true;
      },
    ]) {
      const source = JSON.parse(JSON.stringify(buildWorkerSupervisorLocalDispatchSource()));
      const profile = JSON.parse(JSON.stringify(buildWorkerSupervisorLocalDispatchProfile()));
      mutate(source);
      mutate(profile);
      expect(validateSource(source)).toBe(false);
      expect(validateProfile(profile)).toBe(false);
    }
  });

  it("keeps checked-in source and profile schemas closed", () => {
    expect(JSON.parse(readFileSync(resolve(root, LOCAL_DISPATCH_SOURCE_PATH), "utf8"))).toEqual(
      buildWorkerSupervisorLocalDispatchSource(),
    );
    expect(JSON.parse(readFileSync(resolve(root, LOCAL_DISPATCH_PROFILE_PATH), "utf8"))).toEqual(
      buildWorkerSupervisorLocalDispatchProfile(),
    );
    expect(
      JSON.parse(readFileSync(resolve(root, LOCAL_DISPATCH_SOURCE_SCHEMA_PATH), "utf8")),
    ).toEqual(buildWorkerSupervisorLocalDispatchSourceSchema());
    expect(
      JSON.parse(readFileSync(resolve(root, LOCAL_DISPATCH_PROFILE_SCHEMA_PATH), "utf8")),
    ).toEqual(buildWorkerSupervisorLocalDispatchProfileSchema());
  });
});
