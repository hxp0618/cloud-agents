import Ajv2020 from "ajv/dist/2020.js";
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import {
  buildWorkerLocalDevProfileSuccessor,
  checkWorkerLocalDevProfileSuccessor,
  WORKER_LOCALDEV_SUCCESSOR_PATH,
  WORKER_LOCALDEV_SUCCESSOR_SCHEMA_PATH,
  writeWorkerLocalDevProfileSuccessor,
} from "../../scripts/generate-worker-localdev-profile-successor";

const ROOT = resolve(import.meta.dirname, "../..");

describe("D-058-WORKER-LOCALDEV-SUCCESSOR-000001", () => {
  it("binds immutable D-056/D-057 predecessors and stays pending rebind", () => {
    expect(() => checkWorkerLocalDevProfileSuccessor(ROOT)).not.toThrow();
    const successor = buildWorkerLocalDevProfileSuccessor(ROOT) as Record<string, any>;
    expect(successor.status).toBe("SUCCESSOR_PENDING_REBIND");
    expect(successor.notGateClosure).toBe(true);
    expect(successor.predecessorMutation).toBe("forbidden");
    expect(successor.implementationBoundary).toEqual({
      runtimeRebind: "PENDING",
      providerSideEffects: "FORBIDDEN",
      deployment: "NOT_AUTHORIZED",
      publication: "NOT_AUTHORIZED",
      gateStatus: "ALL_GATES_OPEN",
    });
    expect(successor.successor.bridge.parent).toBe("D-056-WORKER-LOCALDEV-LAUNCHER-000001.r2");
    expect(successor.predecessors.launcher.internalDigests.profileDigest).toBe(
      "sha256:dc83b89cad24104093e86e69d14743ca9bbc1b106113c90ca52bfa9bde04b72e",
    );
    expect(successor.predecessors.bridge.internalDigests.profileDigest).toBe(
      "sha256:ddb4299c44b39afec63e0346da4401c55c3fcc97bb2d826bfa375accebc8aa86",
    );
    expect(
      [
        ...successor.successor.launcher.generatedPaths,
        ...successor.successor.bridge.generatedPaths,
      ].every((path) => !existsSync(resolve(ROOT, path))),
    ).toBe(true);
  });

  it("keeps the checked-in authority schema strict", () => {
    const schema = JSON.parse(
      readFileSync(resolve(ROOT, WORKER_LOCALDEV_SUCCESSOR_SCHEMA_PATH), "utf8"),
    );
    const document = JSON.parse(
      readFileSync(resolve(ROOT, WORKER_LOCALDEV_SUCCESSOR_PATH), "utf8"),
    );
    const validate = new Ajv2020({ strict: true, allErrors: true }).compile(schema);
    expect(validate(document)).toBe(true);
    expect(validate({ ...document, unexpected: true })).toBe(false);
  });

  it("refuses to write while a successor runtime output exists", () => {
    const outputDirectory = resolve(ROOT, "services/worker/localdev-launcher-profile/v2");
    const outputPath = resolve(outputDirectory, "profile.json");
    expect(existsSync(outputDirectory)).toBe(false);
    mkdirSync(outputDirectory, { recursive: true });
    writeFileSync(outputPath, "sentinel\n");
    try {
      expect(() => writeWorkerLocalDevProfileSuccessor(ROOT)).toThrow(
        /successor runtime output exists while rebind is pending/u,
      );
      expect(readFileSync(outputPath, "utf8")).toBe("sentinel\n");
    } finally {
      rmSync(outputDirectory, { recursive: true, force: true });
    }
  });
});
