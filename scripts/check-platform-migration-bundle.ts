import { createHash } from "node:crypto";
import { resolve } from "node:path";

import { validateCheckedInMigrationBundle } from "./lib/platform-migration-bundle";
import { readDeterministicUstar } from "./lib/platform-migration-ustar";

const root = resolve(import.meta.dirname, "..");
const bundle = validateCheckedInMigrationBundle(root);
const runtimeEntries = readDeterministicUstar(bundle.runtimeTar);
const largestRuntimeMember = runtimeEntries.toSorted(
  (left, right) => right.data.length - left.data.length,
)[0]!;
process.stdout.write(
  `${JSON.stringify(
    {
      status: "BOOTSTRAP_VALIDATED",
      notGateClosure: true,
      schemaBundleDigest: bundle.manifest.schema_bundle_digest,
      bootstrapBundleDigest: bundle.manifest.bootstrap_bundle_digest,
      manifestDigest: bundle.manifest.manifest_digest,
      generatedFiles: bundle.files.size,
      deterministicUstarBytes: bundle.runtimeTar.length,
      deterministicUstarLimitBytes: 64 * 1024 * 1024,
      deterministicUstarSha256: `sha256:${createHash("sha256").update(bundle.runtimeTar).digest("hex")}`,
      largestRuntimeMember: {
        path: largestRuntimeMember.path,
        sizeBytes: largestRuntimeMember.data.length,
        jsonMemberLimitBytes: largestRuntimeMember.path.endsWith(".json") ? 1024 * 1024 : null,
      },
      deterministicBootstrapUstarBytes: bundle.bootstrapTar.length,
      deterministicBootstrapUstarSha256: `sha256:${createHash("sha256").update(bundle.bootstrapTar).digest("hex")}`,
      catalogRuntimeIntrospection: "NOT_IMPLEMENTED",
      schemaPublicationStatus: "UNPUBLISHED_BOOTSTRAP_MUTABLE",
      signingAndPublication: "NOT_IMPLEMENTED",
    },
    null,
    2,
  )}\n`,
);
