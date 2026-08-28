import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";

import {
  expectedArtifactCount,
  parsePlatformReleaseOptions,
  platformReleaseArtifact,
  PLATFORM_RELEASE_GO_COMMANDS,
  PLATFORM_RELEASE_RUNTIME,
  PLATFORM_RELEASE_TARGETS,
  type PlatformReleaseArtifact,
  type PlatformReleaseTarget,
  validatePlatformReleaseManifest,
} from "./lib/platform-release.ts";

const repositoryRoot = resolve(import.meta.dirname, "..");
const options = parsePlatformReleaseOptions(process.argv.slice(2), repositoryRoot);
const sourceStatus = run(
  "git",
  ["status", "--porcelain=v1", "--untracked-files=all"],
  repositoryRoot,
);
if (sourceStatus.trim() && !options.allowDirty) {
  throw new Error(
    "platform release requires a clean source tree; use --allow-dirty only for local validation.",
  );
}
if (existsSync(options.outputDirectory))
  throw new Error(`release output already exists: ${options.outputDirectory}`);
mkdirSync(options.outputDirectory, { recursive: true, mode: 0o755 });

const artifacts: PlatformReleaseArtifact[] = [];
for (const target of PLATFORM_RELEASE_TARGETS) {
  for (const command of PLATFORM_RELEASE_GO_COMMANDS) {
    const filename = `${command}-${target}`;
    const output = join(options.outputDirectory, filename);
    buildGoArtifact(command, target, output);
    const bytes = readFileSync(output);
    chmodSync(output, 0o555);
    artifacts.push(platformReleaseArtifact(command, target, filename, bytes));
  }
}

run("bun", ["run", "--cwd", "packages/cloud-agent-distribution", "build"], repositoryRoot);
const runtimeOutput = join(options.outputDirectory, PLATFORM_RELEASE_RUNTIME);
const runtimeBytes = readFileSync(
  join(repositoryRoot, "packages/cloud-agent-distribution/dist/stdio.mjs"),
);
writeFileSync(runtimeOutput, runtimeBytes, { mode: 0o555 });
artifacts.push(
  platformReleaseArtifact(
    "cloud-agent-runtime",
    "portable",
    PLATFORM_RELEASE_RUNTIME,
    runtimeBytes,
  ),
);

artifacts.sort((left, right) => left.filename.localeCompare(right.filename));
if (artifacts.length !== expectedArtifactCount())
  throw new Error("platform release artifact set is incomplete.");
const manifest = {
  schemaVersion: 1 as const,
  kind: "cloud-agents-platform-release" as const,
  version: options.version,
  sourceCommit: run("git", ["rev-parse", "HEAD"], repositoryRoot).trim(),
  sourceDirty: sourceStatus.trim() !== "",
  artifacts,
};
const manifestBytes = Buffer.from(`${JSON.stringify(manifest, null, 2)}\n`);
validatePlatformReleaseManifest(manifest);
writeFileSync(join(options.outputDirectory, "platform-release-manifest.json"), manifestBytes, {
  mode: 0o444,
});
writeFileSync(
  join(options.outputDirectory, "checksums.sha256"),
  `${artifacts.map((artifact) => `${artifact.sha256.slice("sha256:".length)}  ${artifact.filename}`).join("\n")}\n`,
  { mode: 0o444 },
);
process.stdout.write(`${JSON.stringify(manifest, null, 2)}\n`);

function buildGoArtifact(command: string, target: PlatformReleaseTarget, output: string): void {
  const [goos, goarch] = target.split("-") as [string, string];
  const module = command === "cloud-agents-worker" ? "services/worker" : "services/control-plane";
  run(
    "go",
    ["-C", module, "build", "-trimpath", "-ldflags=-buildid=", "-o", output, `./cmd/${command}`],
    repositoryRoot,
    {
      GOOS: goos,
      GOARCH: goarch,
      CGO_ENABLED: "0",
      GOTOOLCHAIN: "local",
      GOWORK: join(repositoryRoot, "go.work"),
      GOFLAGS: "-mod=readonly",
    },
  );
}

function run(
  command: string,
  args: ReadonlyArray<string>,
  cwd: string,
  extraEnv: Record<string, string> = {},
): string {
  const result = spawnSync(command, [...args], {
    cwd,
    encoding: "utf8",
    env: { ...process.env, ...extraEnv },
  });
  if (result.error) throw result.error;
  if (result.status !== 0)
    throw new Error(`${command} ${args.join(" ")} failed:\n${result.stderr}`);
  return result.stdout;
}
