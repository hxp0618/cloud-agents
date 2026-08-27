import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import Ajv2020 from "ajv/dist/2020.js";
import { describe, expect, it } from "vitest";

import {
  buildMigrationRunnerBinding,
  RUNNER_BINDING_GO_PATH,
  RUNNER_BINDING_PROFILE_PATH,
  RUNNER_BINDING_PROFILE_SCHEMA_PATH,
  RUNNER_BINDING_SOURCE_PATH,
  RUNNER_BINDING_SOURCE_SCHEMA_PATH,
  validateCheckedInMigrationRunnerBinding,
} from "./platform-migration-runner-binding";
import {
  canonicalizeMigrationJson,
  parseStrictMigrationJson,
  type MigrationJson,
} from "./platform-migration-json";

const root = resolve(import.meta.dirname, "../..");

describe("D-053-MIG-000014.r2 localdev runner binding", () => {
  it("is generated from an immutable r1 closure and the two closed selectors", () => {
    const binding = validateCheckedInMigrationRunnerBinding(root);
    expect(binding.profile.authorityId).toBe("D-053-MIG-000014");
    expect(binding.profile.revision).toBe("D-053-MIG-000014.r2");
    expect(binding.profile.runner).toMatchObject({
      mode: "localdev_only",
      bindBeforeConnect: true,
      completeLedger: "no-op",
      entryWriter: "NOT_IMPLEMENTED",
      recoveryWriter: "NOT_IMPLEMENTED",
      externalEffects: "forbidden",
    });
    expect(binding.profile.implementationBoundary).toMatchObject({
      databaseWrites: "not_authorized",
      productionRunner: "forbidden",
      http: "forbidden",
      p2: "forbidden",
      provider: "forbidden",
      deployment: "forbidden",
      publication: "forbidden",
      gateTransition: "forbidden",
    });
    const selectors = binding.profile.selectors as Array<Record<string, any>>;
    expect(selectors.map((selector) => selector.selectorId)).toEqual([
      "canonical-000013",
      "successor-000014",
    ]);
    expect(selectors.map((selector) => selector.schemaHead)).toEqual(["000013", "000014"]);
    expect(selectors[0]?.migrationCount).toBe(13);
    expect(selectors[1]?.migrationCount).toBe(14);
    expect(selectors[0]?.artifactSet).toHaveLength(40);
    expect(selectors[1]?.artifactSet).toHaveLength(43);
    for (const selector of selectors) {
      expect(
        new Set((selector.artifactSet as Array<Record<string, unknown>>).map((entry) => entry.path))
          .size,
      ).toBe((selector.artifactSet as Array<unknown>).length);
      expect(selector.completeLedger).toBe("no-op");
      expect(selector.entryWriter).toBe("NOT_IMPLEMENTED");
      expect(selector.recoveryWriter).toBe("NOT_IMPLEMENTED");
    }
    expect(binding.source.r1Closure).toMatchObject({
      inputPaths: expect.arrayContaining([
        "services/control-plane/internal/localmigration/localmigration.go",
      ]),
      protectedPaths: expect.arrayContaining(["services/control-plane/migrations/manifest.json"]),
      exclusionPaths: expect.arrayContaining(["go.work"]),
    });
    expect((binding.source.r1Closure.inputPaths as unknown[]).length).toBe(167);
    expect((binding.source.r1Closure.protectedPaths as unknown[]).length).toBe(29);
    expect((binding.source.r1Closure.exclusionPaths as unknown[]).length).toBe(14);
    expect(binding.source.predecessorAuthority).toMatchObject({
      git: {
        commit: "1325dc1773ef9bad2d809fedee9b392e3cdbf959",
        tree: "49e53f2462af20201231c2428eb56cce543403a2",
        subtree: {
          path: "services/control-plane/migrations/successor/000014",
          sha1: "9d704eec0c8ca04fc0f1bd41b4a348db0b853096",
        },
        blobs: {
          authoritySource: {
            path: "services/control-plane/migrations/successor/000014/authority-source.json",
            sha1: "040c765971adde44d2171382d726ff294de05954",
          },
          authoritySourceSchema: {
            path: "services/control-plane/migrations/successor/000014/authority-source.schema.json",
            sha1: "34705605fb42f049135da3a31a911387820f872f",
          },
          profile: {
            path: "services/control-plane/migrations/successor/000014/profile.json",
            sha1: "046e2c51581964a59e770308da4a9fe23635f3ee",
          },
          profileSchema: {
            path: "services/control-plane/migrations/successor/000014/profile.schema.json",
            sha1: "124ea8dda97838f14cc9512fa06022b55ca74f87",
          },
        },
      },
    });
  }, 30_000);

  it("freezes both raw and logical identities for the r1 objects", () => {
    const source = readFileSync(
      resolve(root, "services/control-plane/migrations/successor/000014/authority-source.json"),
    );
    const profile = readFileSync(
      resolve(root, "services/control-plane/migrations/successor/000014/profile.json"),
    );
    expect(digest(source)).toBe(
      "sha256:6436c991dc838c353f27f91f9aff3257d02e18a6c3e0535244fe7f7d1d7a5d8e",
    );
    expect(digest(profile)).toBe(
      "sha256:668c7e9c0337d1e50c81dde0ac465561d4ac4eb5f6d14f7fd8b2e26ef672250a",
    );
    const binding = buildMigrationRunnerBinding(root);
    const profileObject = parseStrictMigrationJson(profile) as Record<string, MigrationJson>;
    const profileDigest = String(profileObject.profileDigest);
    const profileBody = Object.fromEntries(
      Object.entries(profileObject).filter(([key]) => key !== "profileDigest"),
    ) as MigrationJson;
    expect(domainDigest("cloud-agents-platform-migration-successor/v1", profileBody)).toBe(
      profileDigest,
    );
    expect(binding.source.predecessorAuthority).toMatchObject({
      authorityId: "D-053-MIG-000014",
      revision: "D-053-MIG-000014.r1",
      profileLogicalDigest:
        "sha256:0637e32e1e07d82ff2917a13f8ade6276c2518ff0aeb7a80451f9da0f69b2630",
    });
  });

  it("keeps schemas closed Draft 2020-12 and all generated outputs current", () => {
    const binding = validateCheckedInMigrationRunnerBinding(root);
    for (const schemaPath of [
      RUNNER_BINDING_SOURCE_SCHEMA_PATH,
      RUNNER_BINDING_PROFILE_SCHEMA_PATH,
    ]) {
      const schema = parseStrictMigrationJson(readFileSync(resolve(root, schemaPath))) as Record<
        string,
        MigrationJson
      >;
      expect(schema.$schema).toBe("https://json-schema.org/draft/2020-12/schema");
      expect(typeof schema.$id).toBe("string");
      expect(binding.generatedFiles.has(schemaPath)).toBe(true);
    }
    expect(binding.generatedFiles.has(RUNNER_BINDING_SOURCE_PATH)).toBe(true);
    expect(binding.generatedFiles.has(RUNNER_BINDING_PROFILE_PATH)).toBe(true);
    expect(binding.generatedFiles.has(RUNNER_BINDING_GO_PATH)).toBe(true);
  });

  it("rejects selector descriptor identity drift at schema level", () => {
    const ajv = new Ajv2020({ strict: true, allErrors: true, validateFormats: false });
    for (const [documentName, schemaName] of [
      ["authority-source", "authority-source.schema"],
      ["profile", "profile.schema"],
    ] as const) {
      const document = JSON.parse(
        readFileSync(
          resolve(
            root,
            `services/control-plane/migrations/successor/000014/runner-binding/${documentName}.json`,
          ),
          "utf8",
        ),
      ) as Record<string, any>;
      const schema = JSON.parse(
        readFileSync(
          resolve(
            root,
            `services/control-plane/migrations/successor/000014/runner-binding/${schemaName}.json`,
          ),
          "utf8",
        ),
      ) as Record<string, any>;
      const validate = ajv.compile(schema);
      expect(validate(document), `${documentName} base document rejected`).toBe(true);
      for (const mutate of [
        (value: Record<string, any>) => {
          value.selectors[0].manifest.path = "foreign/manifest.json";
        },
        (value: Record<string, any>) => {
          value.selectors[0].manifestDigest = `sha256:${"0".repeat(64)}`;
        },
        (value: Record<string, any>) => {
          value.selectors[1].schemaBundle.sizeBytes = 1;
        },
        (value: Record<string, any>) => {
          value.selectors[1].schemaBundleDigest = `sha256:${"0".repeat(64)}`;
        },
        (value: Record<string, any>) => {
          value.predecessorAuthority.git.commit = "0".repeat(40);
        },
        (value: Record<string, any>) => {
          value.r1Closure.git.blobs.successorManifest.path = "foreign/manifest.json";
        },
      ]) {
        const mutated = JSON.parse(JSON.stringify(document)) as Record<string, any>;
        mutate(mutated);
        expect(validate(mutated), `${documentName} schema accepted identity drift`).toBe(false);
      }
    }
  });
});

function digest(value: Uint8Array): string {
  return `sha256:${createHash("sha256").update(value).digest("hex")}`;
}

function domainDigest(domain: string, value: MigrationJson): string {
  return `sha256:${createHash("sha256").update(domain).update("\0").update(canonicalizeMigrationJson(value)).digest("hex")}`;
}
