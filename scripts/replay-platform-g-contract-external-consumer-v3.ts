import { resolve } from "node:path";

import {
  buildExternalConsumerV3ProjectionPlan,
  buildExternalConsumerV3ReplayPlan,
  EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH,
} from "./lib/platform-g-contract-external-consumer-v3";
import { writeExternalConsumerV3Projection } from "./lib/platform-g-contract-external-consumer-v3-projection";
import { runExternalConsumerV3NativeReplay } from "./lib/platform-g-contract-external-consumer-v3-native";

import {
  assertExternalConsumerV3SummaryCurrent,
  writeExternalConsumerV3Summary,
} from "./lib/platform-g-contract-external-consumer-v3-summary";

const root = resolve(import.meta.dirname, "..");
const args = process.argv.slice(2);
const mode = args[0];

function flag(name: string): string {
  const index = args.indexOf(name);
  const value = index >= 0 ? args[index + 1] : undefined;
  if (!value || value.startsWith("--")) throw new Error(`Missing value for ${name}.`);
  return value;
}

if (mode === "--check-authority") {
  const plan = buildExternalConsumerV3ReplayPlan(root);
  process.stdout.write(
    `g-contract-external-consumer-v3-replay: authority current; status=${plan.status}; replay pending; runner=${EXTERNAL_CONSUMER_V3_REPLAY_RUNNER_PATH}\n`,
  );
  process.exitCode = 0;
} else if (mode === "--plan") {
  process.stdout.write(`${JSON.stringify(buildExternalConsumerV3ReplayPlan(root), null, 2)}\n`);
} else if (mode === "--projection") {
  if (args.length === 1) {
    process.stdout.write(
      `${JSON.stringify(buildExternalConsumerV3ProjectionPlan(root), null, 2)}\n`,
    );
  } else {
    const result = writeExternalConsumerV3Projection({
      authorityRoot: root,
      candidateRoot: flag("--candidate-root"),
      candidateManifestPath: flag("--candidate-manifest"),
      outputRoot: flag("--output-root"),
    });
    process.stdout.write(`${JSON.stringify(result.receipt, null, 2)}\n`);
  }
} else if (mode === "--summary") {
  const result = writeExternalConsumerV3Summary({
    authorityRoot: root,
    projectionRoot: flag("--projection-root"),
    nativeRoots: {
      "darwin-arm64-A": flag("--darwin-a-root"),
      "darwin-arm64-B": flag("--darwin-b-root"),
      "linux-amd64-A": flag("--linux-a-root"),
      "linux-amd64-B": flag("--linux-b-root"),
    },
    outputRoot: flag("--output-root"),
  });
  process.stdout.write(
    `${JSON.stringify(
      {
        summaryPath: result.summaryPath,
        profilePath: result.profilePath,
        status: result.profile.status,
        gateStatus: result.profile.gateStatus,
      },
      null,
      2,
    )}\n`,
  );
} else if (mode === "--check-summary") {
  assertExternalConsumerV3SummaryCurrent({
    authorityRoot: root,
    outputRoot: flag("--output-root"),
  });
  process.stdout.write(
    "g-contract-external-consumer-v3: summary/profile current; final review pending; ALL_GATES_OPEN\n",
  );
} else if (mode === "--native") {
  const platform = flag("--platform");
  const runId = flag("--run-id");
  if (platform !== "darwin-arm64" && platform !== "linux-amd64") {
    throw new Error(`Unsupported native replay platform: ${platform}`);
  }
  if (runId !== "A" && runId !== "B") {
    throw new Error(`Unsupported native replay run id: ${runId}`);
  }
  const result = runExternalConsumerV3NativeReplay({
    authorityRoot: root,
    projectionRoot: flag("--projection-root"),
    outputRoot: flag("--output-root"),
    platform,
    runId,
  });
  process.stdout.write(`${JSON.stringify(result.receipt, null, 2)}\n`);
} else {
  throw new Error(
    "Usage: bun scripts/replay-platform-g-contract-external-consumer-v3.ts --check-authority|--plan|--check-summary --output-root <path>|--summary --projection-root <path> --darwin-a-root <path> --darwin-b-root <path> --linux-a-root <path> --linux-b-root <path> --output-root <path>|--projection [--candidate-root <path> --candidate-manifest <path> --output-root <path>]|--native --projection-root <path> --platform <darwin-arm64|linux-amd64> --run-id <A|B> --output-root <path>",
  );
}
