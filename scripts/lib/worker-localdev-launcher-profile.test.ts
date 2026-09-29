import Ajv2020 from "ajv/dist/2020.js";
import { describe, expect, it } from "vitest";
import {
  buildWorkerLocalDevLauncherSource,
  buildWorkerLocalDevLauncherSourceSchema,
} from "./worker-localdev-launcher-profile";
describe("D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1", () => {
  it("rejects input path drift and undeclared members", () => {
    const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: false });
    const validate = ajv.compile(buildWorkerLocalDevLauncherSourceSchema());
    const appended = buildWorkerLocalDevLauncherSource() as Record<string, any>;
    appended.inputPaths = [...appended.inputPaths, "untracked.txt"];
    expect(validate(appended)).toBe(false);
    const duplicate = buildWorkerLocalDevLauncherSource() as Record<string, any>;
    duplicate.inputPaths = [duplicate.inputPaths[0], ...duplicate.inputPaths];
    expect(validate(duplicate)).toBe(false);
    const extra = buildWorkerLocalDevLauncherSource() as Record<string, any>;
    extra.unexpected = true;
    expect(validate(extra)).toBe(false);
  });
});
