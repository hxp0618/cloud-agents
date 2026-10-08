import Ajv2020 from "ajv/dist/2020.js";
import { describe, expect, it } from "vitest";
import {
  assertWorkerLocalDevBridgeParentLineage,
  buildWorkerLocalDevBridgeSource,
  buildWorkerLocalDevBridgeSourceSchema,
  WORKER_LOCALDEV_BRIDGE_PARENT_IDENTITY,
} from "../../scripts/lib/worker-localdev-bridge-profile";
describe("D-057-WORKER-LOCALDEV-BRIDGE-000001.r1", () => {
  it("rejects input path drift and undeclared members", () => {
    const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: false });
    const validate = ajv.compile(buildWorkerLocalDevBridgeSourceSchema());
    const appended = buildWorkerLocalDevBridgeSource() as Record<string, any>;
    appended.inputPaths = [...appended.inputPaths, "untracked.txt"];
    expect(validate(appended)).toBe(false);
    const duplicate = buildWorkerLocalDevBridgeSource() as Record<string, any>;
    duplicate.inputPaths = [duplicate.inputPaths[0], ...duplicate.inputPaths];
    expect(validate(duplicate)).toBe(false);
    const extra = buildWorkerLocalDevBridgeSource() as Record<string, any>;
    extra.unexpected = true;
    expect(validate(extra)).toBe(false);
  });

  it("rejects parent profile identity drift", () => {
    const parent = { ...WORKER_LOCALDEV_BRIDGE_PARENT_IDENTITY };
    expect(() => assertWorkerLocalDevBridgeParentLineage(parent)).not.toThrow();
    expect(() =>
      assertWorkerLocalDevBridgeParentLineage({
        ...parent,
        profileDigest: `sha256:${"0".repeat(64)}`,
      }),
    ).toThrow(/profileDigest mismatch/);
  });
});
