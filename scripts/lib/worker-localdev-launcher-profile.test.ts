import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import { describe, expect, it } from "vitest";
import {
  assertWorkerLocalDevLauncherCurrent,
  buildWorkerLocalDevLauncherProfile,
  buildWorkerLocalDevLauncherProfileSchema,
  buildWorkerLocalDevLauncherSource,
  buildWorkerLocalDevLauncherSourceSchema,
  WORKER_LOCALDEV_LAUNCHER_PROFILE_PATH,
  WORKER_LOCALDEV_LAUNCHER_SOURCE_PATH,
} from "./worker-localdev-launcher-profile";

const root = resolve(import.meta.dirname, "../..");
describe("D-056-WORKER-LOCALDEV-LAUNCHER-000001.r1", () => {
  it("is deterministic, localdev-only, and current", () => {
    const source = buildWorkerLocalDevLauncherSource();
    const profile = buildWorkerLocalDevLauncherProfile();
    expect(source.authorityId).toBe("D-056-WORKER-LOCALDEV-LAUNCHER-000001");
    expect(profile.profileId).toBe("cloud-agents/worker-localdev-launcher/v1alpha1");
    expect(profile.mode).toBe("localdev_only");
    expect((profile.externalSideEffects as any).database).toBe(false);
    expect((profile.externalSideEffects as any).provider).toBe(false);
    expect((profile.externalSideEffects as any).runtime).toBe(false);
    expect((profile.selector as any).listenAddress).toBe("loopback_only");
    expect((profile.runner as any).entrypoint).toBe(
      "GOWORK=off GOFLAGS=-mod=readonly go test -tags localdev ./...",
    );
    expect((profile.archive as any).emission).toBe("forbidden");
    expect((profile.memberManifest as any).emission).toBe("forbidden");
    expect(profile.inputManifestDigest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    assertWorkerLocalDevLauncherCurrent(root);
  });
  it("rejects caller-selected identity and side-effect drift", () => {
    const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: false });
    const vs = ajv.compile(buildWorkerLocalDevLauncherSourceSchema());
    const vp = ajv.compile(buildWorkerLocalDevLauncherProfileSchema());
    const source = JSON.parse(
      readFileSync(resolve(root, WORKER_LOCALDEV_LAUNCHER_SOURCE_PATH), "utf8"),
    );
    const profile = JSON.parse(
      readFileSync(resolve(root, WORKER_LOCALDEV_LAUNCHER_PROFILE_PATH), "utf8"),
    );
    expect(vs(source)).toBe(true);
    expect(vp(profile)).toBe(true);
    source.selector.listenAddress = "0.0.0.0";
    profile.externalSideEffects.database = true;
    expect(vs(source)).toBe(false);
    expect(vp(profile)).toBe(false);
  });
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
