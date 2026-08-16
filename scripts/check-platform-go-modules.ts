import { spawnSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import {
  PLATFORM_GO_MODULES,
  PLATFORM_GO_TOOLCHAIN,
  PLATFORM_GO_VERSION,
  type GoModuleEditDocument,
  validateGoModuleEdit,
  validatePlatformGoModuleTree,
} from "./lib/platform-go-modules";

const root = resolve(import.meta.dirname, "..");
const PINNED_GO_ENV = {
  GOTOOLCHAIN: "local",
  GOFLAGS: "-mod=readonly",
} as const;
const ISOLATED_GO_ENV = {
  ...PINNED_GO_ENV,
  GOWORK: "off",
} as const;

const goVersion = run("go", ["version"], root, PINNED_GO_ENV).trim();
if (!goVersion.startsWith(`go version ${PLATFORM_GO_TOOLCHAIN} `)) {
  throw new Error(`Expected ${PLATFORM_GO_TOOLCHAIN}, found ${goVersion}.`);
}
validatePlatformGoModuleTree(root);

const workspace = JSON.parse(run("go", ["work", "edit", "-json"], root, PINNED_GO_ENV)) as {
  Go?: string;
  Use?: ReadonlyArray<{ DiskPath?: string }>;
  Replace?: ReadonlyArray<unknown> | null;
};
const workspaceSource = readFileSync(resolve(root, "go.work"), "utf8");
const workspaceToolchains = [...workspaceSource.matchAll(/^\s*toolchain\s+([^\s]+)\s*$/gmu)].map(
  (match) => match[1],
);
if (
  workspace.Go !== PLATFORM_GO_VERSION ||
  workspaceToolchains.length !== 1 ||
  workspaceToolchains[0] !== PLATFORM_GO_TOOLCHAIN
) {
  throw new Error(
    `go.work must pin go ${PLATFORM_GO_VERSION} and ${PLATFORM_GO_TOOLCHAIN}, found ${String(workspace.Go)} / ${workspaceToolchains.join(",")}.`,
  );
}
const actualUses = (workspace.Use ?? []).map((entry) => entry.DiskPath).toSorted();
const expectedUses = PLATFORM_GO_MODULES.map((entry) => `./${entry.directory}`).toSorted();
if (JSON.stringify(actualUses) !== JSON.stringify(expectedUses)) {
  throw new Error(`go.work use set mismatch: ${JSON.stringify(actualUses)}.`);
}
if ((workspace.Replace?.length ?? 0) !== 0) {
  throw new Error("go.work must not contain replace directives.");
}

for (const entry of PLATFORM_GO_MODULES) {
  const directory = resolve(root, entry.directory);
  const module = JSON.parse(
    run("go", ["mod", "edit", "-json"], directory, ISOLATED_GO_ENV),
  ) as GoModuleEditDocument;
  validateGoModuleEdit(module, entry.module, `${entry.directory}/go.mod`);
  run("go", ["mod", "tidy", "-diff"], directory, ISOLATED_GO_ENV);
  run("go", ["test", "-timeout=30m", "./..."], directory, ISOLATED_GO_ENV);
}

process.stdout.write(
  `platform-go-modules: ${PLATFORM_GO_MODULES.length} modules, ${PLATFORM_GO_TOOLCHAIN}, GOWORK=off, GOTOOLCHAIN=local, GOFLAGS=-mod=readonly PASS\n`,
);

function run(
  command: string,
  args: ReadonlyArray<string>,
  cwd: string,
  environment: Readonly<Record<string, string>> = {},
): string {
  const result = spawnSync(command, args, {
    cwd,
    encoding: "utf8",
    env: { ...process.env, ...environment },
  });
  if (result.error) throw result.error;
  if (result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} failed (${String(result.status)}):\n${result.stdout}${result.stderr}`,
    );
  }
  return result.stdout;
}
