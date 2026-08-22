import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, test } from "bun:test";

import { buildRunnerLedgerConsumerRegistry } from "./platform-runner-ledger-consumer-registry";
import {
  assertRunnerLedgerRecoveryRegistriesCurrent,
  buildRunnerLedgerRecoveryRegistries,
  expectedRunnerLedgerRecoverySourceSuite,
  RUNNER_LEDGER_RECOVERY_FAMILIES,
  RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS,
  RUNNER_LEDGER_RECOVERY_PAIR_BINDINGS,
  RUNNER_LEDGER_RECOVERY_SOURCE_PATH,
  validateRunnerLedgerRecoveryFixture,
} from "./platform-runner-ledger-recovery-registry";

const root = resolve(import.meta.dirname, "../..");

function readJson(path: string): Record<string, any> {
  return JSON.parse(readFileSync(resolve(root, path), "utf8")) as Record<string, any>;
}

function fileSha256(path: string): string {
  return createHash("sha256")
    .update(readFileSync(resolve(root, path)))
    .digest("hex");
}

describe("runner ledger recovery generated registry suite", () => {
  test("is current, ordered, and bound to the exact immutable predecessor identities", () => {
    expect(() => assertRunnerLedgerRecoveryRegistriesCurrent(root)).not.toThrow();
    expect(readJson(RUNNER_LEDGER_RECOVERY_SOURCE_PATH)).toEqual(
      expectedRunnerLedgerRecoverySourceSuite(),
    );
    const consumer = buildRunnerLedgerConsumerRegistry(root) as Record<string, any>;
    const registries = buildRunnerLedgerRecoveryRegistries(root) as Array<Record<string, any>>;
    expect(registries).toHaveLength(8);
    expect(registries.map((item) => item.family)).toEqual(RUNNER_LEDGER_RECOVERY_FAMILIES);
    expect(registries[0]?.predecessorBinding).toMatchObject({
      registryId: consumer.registryId,
      registryDigest: consumer.registryDigest,
      profileId: "runner-ledger-consumer/v1",
      profileDigest: consumer.profile.profileDigest,
    });
    expect(registries[0]?.historicalBindings.map((item: any) => item.profileId)).toEqual([
      "runner-ledger-preflight/v1",
      "runner-ledger-consumer/v1",
      "runner-ledger-entry-admission/v1",
      "runner-ledger-entry-execution-admission/v1",
      "runner-ledger-entry-success-writer/v1",
    ]);
    for (const [index, family] of RUNNER_LEDGER_RECOVERY_FAMILIES.entries()) {
      expect(readJson(RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS[family])).toEqual(registries[index]);
    }
  });

  test("closes the exact twelve-pair mapping without a union writer", () => {
    const registries = buildRunnerLedgerRecoveryRegistries(root) as Array<Record<string, any>>;
    const counts = registries.map((item) => item.profile.spec.pairBindings.length);
    expect(counts).toEqual([12, 4, 1, 1, 1, 3, 0, 2]);
    expect(registries[0]?.profile.spec.pairBindings).toEqual(RUNNER_LEDGER_RECOVERY_PAIR_BINDINGS);
    const uniquePairs = new Set(
      RUNNER_LEDGER_RECOVERY_PAIR_BINDINGS.map(
        (item) => `${item.disposition}\u0000${item.state}\u0000${item.action}`,
      ),
    );
    expect(uniquePairs.size).toBe(12);
    const execution = registries[5]!;
    const writer = registries[6]!;
    expect(execution.profile.spec.profileId).toBe("runner-ledger-recovery-execution-admission/v1");
    expect(writer.profile.spec.profileId).toBe("runner-ledger-recovery-success-writer/v1");
    expect(writer.predecessorBinding.profileId).toBe(execution.profile.spec.profileId);
    expect(writer.predecessorBinding.registryDigest).toBe(execution.registryDigest);
    expect(writer.profile.spec.permitFromProfileId).toBe(execution.profile.spec.profileId);
    expect(writer.profile.spec.pairBindings).toEqual([]);
    expect(writer.registryId).not.toBe(execution.registryId);
    expect(writer.registryDigest).not.toBe(execution.registryDigest);
    expect(writer.profile.profileDigest).not.toBe(execution.profile.profileDigest);
  });

  test("preserves every historical runner contract and generated Go profile byte-for-byte", () => {
    const expected: Record<string, string> = {
      "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-preflight-registry-source-v1.json":
        "bd1a9e57fd5f1014a7afead056d6c03f1b0a8501e9767e1eb0308aef9065bd71",
      "contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-source-v1.schema.json":
        "2c48a4db4641de750336fb2cfb454da98998a494002342f29201c17dfdbc7204",
      "contracts/platform/v1alpha1/schemas/runner-ledger-preflight-registry-v1.schema.json":
        "829b9e7aefaf16642090051b93babb311790a3be354f91ed91520cca39079c5c",
      "contracts/generated/platform/v1alpha1/runner-ledger-preflight-registry-v1.json":
        "2a04f67f9b06f25bc13211934d8cb914dcbe3f92d42053f636ec66b4f28ac11c",
      "services/control-plane/internal/migration/runner_ledger_preflight_profile_generated.go":
        "599b78537a3f1dd5d70c1b50aa5e7bc54e1b65ee463876ee3f111709a5ab2112",
      "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-consumer-registry-source-v1.json":
        "3b81553a58077bb1e748f7f4f6474c59ac9d8dcfb5fdbffd1cab00d7d4361b64",
      "contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-source-v1.schema.json":
        "c1a82d48448a38c94d613d05b28a933ac95986ea038e85f35bac4d0590387120",
      "contracts/platform/v1alpha1/schemas/runner-ledger-consumer-registry-v1.schema.json":
        "bb8f9557621825f45150a4f1b3b7708566f3a0e790077307406fd287b6a86ae6",
      "contracts/generated/platform/v1alpha1/runner-ledger-consumer-registry-v1.json":
        "fa7082803ea97d06eefa83eec3de784f7199fc0b47f0ca2d0f8203b8b7e96852",
      "services/control-plane/internal/migration/runner_ledger_consumer_profile_generated.go":
        "afc77e723b7a4439c47043376cb79f5cb6416ce22d54ab1dcffbfe49686ce928",
      "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-admission-registry-source-v1.json":
        "56fcaa4806731ff968c4614f710502b39dad66ee64784662bf962f58a37c3b88",
      "contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-source-v1.schema.json":
        "5bd4c267e62d87287ae68a0f6f4e8a2d2dbf3e65af0a96d522019342e2abb17b",
      "contracts/platform/v1alpha1/schemas/runner-ledger-entry-admission-registry-v1.schema.json":
        "bbe0c63c2942b8286fca6daca546c54b4c43efd0ab7193cd0c4e10ef6e27d409",
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-admission-registry-v1.json":
        "2dc0210f1aad1dd6cff1183324837ab7e88cc5491e9046ae07302b25a1f9e372",
      "services/control-plane/internal/migration/runner_ledger_entry_admission_profile_generated.go":
        "c95850d99a5cbc9d82480d2c63befd6a39ffaeeb2f2c2f1374ce21091ff806c6",
      "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-execution-admission-registry-source-v1.json":
        "88bbb305ced88107407a830b195208c1c02bb1f5bc7a321c2c4a17042b37ecbb",
      "contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-source-v1.schema.json":
        "505fdcd72f113d9156c5549ac0ef02c97c4d9e40286495082753cb98d7ae8d9a",
      "contracts/platform/v1alpha1/schemas/runner-ledger-entry-execution-admission-registry-v1.schema.json":
        "96eb821ce315b23540146a7e9b77cfbac9b68e12da1367d9f6f054ed61b20d97",
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-execution-admission-registry-v1.json":
        "9ef15ce291207580d7bc0426d22d7e4e5a43260a89ea5375c5f8e10e08c0dc96",
      "contracts/platform/v1alpha1/fixtures/golden/runner-ledger-entry-success-writer-registry-source-v1.json":
        "ee114d994062d0f3c6ee9f96a1d962621a5f95f19eba3b21a5de6bfeb1700db9",
      "contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-source-v1.schema.json":
        "ee23116e5de2d052f8f25fb2addd8cb98bd055901f34f222aa9561437e5d3274",
      "contracts/platform/v1alpha1/schemas/runner-ledger-entry-success-writer-registry-v1.schema.json":
        "2e6f4a49f734983b2e3f57074814c57be3ff7f596e8df35cfb527436b0274beb",
      "contracts/generated/platform/v1alpha1/runner-ledger-entry-success-writer-registry-v1.json":
        "0025cb5a4f38644848bf5317f37b8b849fc5861f56872ff6c2bd860bd841a5e6",
      "services/control-plane/internal/migration/runner_ledger_entry_writer_profile_generated.go":
        "63b2e2ac4aec2f02ba9bfc5e90ef716d3659decbbb2ffe716cfe50f189b77c5d",
    };
    for (const [path, digest] of Object.entries(expected)) {
      expect(fileSha256(path), path).toBe(digest);
    }
  });

  test("rejects source order, pair, predecessor, union-writer, and profile drift", () => {
    const source = readJson(RUNNER_LEDGER_RECOVERY_SOURCE_PATH);
    const cases: Array<[string, (candidate: Record<string, any>) => void]> = [
      [
        "profile order",
        (candidate) => {
          [candidate.profiles[0], candidate.profiles[1]] = [
            candidate.profiles[1],
            candidate.profiles[0],
          ];
        },
      ],
      [
        "pair action",
        (candidate) => {
          candidate.profiles[0].pairBindings[0].profileAction = "prepare_retry_handoff";
        },
      ],
      [
        "predecessor",
        (candidate) => {
          candidate.profiles[5].predecessorProfileId = "runner-ledger-entry-admission/v1";
        },
      ],
      [
        "union writer",
        (candidate) => {
          candidate.profiles[6].pairBindings.push(candidate.profiles[0].pairBindings[0]);
        },
      ],
      [
        "permit binding",
        (candidate) => {
          candidate.profiles[6].permitFromProfileId = "runner-ledger-recovery-admission/v1";
        },
      ],
      [
        "profile identity",
        (candidate) => {
          candidate.profiles[7].profileId = "runner-ledger-return-failure/v2";
        },
      ],
    ];
    for (const [name, mutate] of cases) {
      const candidate = structuredClone(source);
      mutate(candidate);
      const result = validateRunnerLedgerRecoveryFixture(candidate, root);
      expect(result.valid, name).toBeFalse();
      if (!result.valid) {
        expect(result.errors[0]?.code, name).toBe("RUNNER_LEDGER_RECOVERY_BINDING_MISMATCH");
      }
    }
  });

  test("rejects edited generated registry identity and policy bytes", () => {
    for (const family of RUNNER_LEDGER_RECOVERY_FAMILIES) {
      const candidate = readJson(RUNNER_LEDGER_RECOVERY_OUTPUT_PATHS[family]);
      candidate.profile.spec.identityBindings.crossProfileRejection = "allowed";
      const result = validateRunnerLedgerRecoveryFixture(candidate, root);
      expect(result.valid, family).toBeFalse();
      if (!result.valid) {
        expect(result.errors[0]?.code, family).toBe(
          "RUNNER_LEDGER_RECOVERY_REGISTRY_DIGEST_MISMATCH",
        );
      }
    }
  }, 30_000);
});
