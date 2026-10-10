// One-command update of an existing single-host Compose development deployment.
//
// Usage: CLOUD_AGENTS_DEPLOY_TARGET=<ssh-destination> bun run deploy:dev [--dry-run] [--all] [--yes]
//
// The host keeps its deployment-owned state (env file, secrets, override) in
// $CLOUD_AGENTS_DEPLOY_ROOT/state. Every image tag is a digest of that image's
// exact build inputs, so only services whose inputs changed are rebuilt, and
// `docker compose up` recreates only containers whose image or config changed.
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { createInterface } from "node:readline/promises";

import {
  buildPlatformDeploymentPackage,
  buildPlatformMigrationPackage,
  platformGoBinary,
  platformGoBuildCommand,
  PLATFORM_RELEASE_DEPLOYMENT,
  PLATFORM_RELEASE_MIGRATION_HEAD,
  PLATFORM_RELEASE_MIGRATIONS,
  type PlatformReleaseTarget,
} from "./lib/platform-release.ts";
import { PLATFORM_GO_TOOLCHAIN } from "./lib/platform-go-modules.ts";
import { buildWorkerOciSupplyArtifacts } from "./lib/worker-oci-supply.ts";

type ImageSpec = Readonly<{
  service: string;
  command?: string;
  aliases: readonly string[];
  dockerfile: string;
  context: "release" | "deploy";
  inputs: (arch: string) => readonly string[];
}>;

const goImage = (service: string, command: string): ImageSpec => ({
  service,
  command,
  aliases: service === "identity" ? ["identity-initialize"] : [],
  dockerfile: `${service}.Dockerfile`,
  context: "release",
  inputs: (arch) => [
    `${command}-linux-${arch}`,
    ...(service === "migrate" ? [PLATFORM_RELEASE_MIGRATIONS] : []),
  ],
});
const webImage = (service: "admin-web" | "user-web"): ImageSpec => ({
  service,
  aliases: [],
  dockerfile: `${service}.Dockerfile`,
  context: "deploy",
  inputs: () => ["web/server.mjs", "web/platform.mjs", "web/index.mjs", `${service}/dist`],
});
const images: readonly ImageSpec[] = [
  goImage("migrate", "cloud-agents-product-migrate"),
  goImage("identity", "cloud-agents-identity"),
  goImage("control-plane", "cloud-agents-control-plane"),
  goImage("access-gateway", "cloud-agents-access-gateway"),
  webImage("admin-web"),
  webImage("user-web"),
];
const goCommands = images.flatMap(({ command }) => (command === undefined ? [] : [command]));
const runningServices = [
  "postgres",
  "identity",
  "control-plane",
  "access-gateway",
  "admin-web",
  "user-web",
];
const stateFiles = ["compose.env", "compose.images.yml", "compose.sh"] as const;

class DeployError extends Error {}

const sshOptions = ["-o", "BatchMode=yes", "-o", "ConnectTimeout=10"];
const repositoryRoot = resolve(import.meta.dirname, "..");
const args = new Set(process.argv.slice(2));
const target = process.env.CLOUD_AGENTS_DEPLOY_TARGET ?? "";
const remoteRoot = process.env.CLOUD_AGENTS_DEPLOY_ROOT ?? "/opt/cloud-agents";
const stateDir = `${remoteRoot}/state`;
const stamp = `${Number(PLATFORM_RELEASE_MIGRATION_HEAD)}-dev.${new Date()
  .toISOString()
  .replace(/[-:]/gu, "")
  .replace(/\.\d+Z$/u, "")}`;
const releaseName = `release-${stamp}`;
const deploymentName = `deployment-${stamp}`;
const stage = join(repositoryRoot, ".tmp/deploy-dev", stamp);

try {
  await main();
} catch (error) {
  if (!(error instanceof DeployError)) throw error;
  process.stderr.write(`deploy-dev: ${error.message}\n`);
  process.exitCode = 1;
} finally {
  rmSync(stage, { recursive: true, force: true });
}

async function main(): Promise<void> {
  for (const arg of args) {
    if (!["--dry-run", "--all", "--yes"].includes(arg)) fail(`unknown argument: ${arg}`);
  }
  if (target === "") {
    fail(
      "set CLOUD_AGENTS_DEPLOY_TARGET to an SSH destination or ~/.ssh/config alias, for example in the git-ignored .env.local",
    );
  }
  const rebuildAll = args.has("--all");

  // 1. Read the current host deployment and resolve base images available offline.
  step(`Inspecting ${target}:${remoteRoot}`);
  const baseImages = new Map(
    images.map(({ dockerfile }) => {
      const source = readFileSync(join(repositoryRoot, "deploy/docker", dockerfile), "utf8");
      const base = source.match(/^ARG BASE_IMAGE=(\S+)$/mu)?.[1];
      if (base === undefined) fail(`${dockerfile} has no ARG BASE_IMAGE default`);
      return [dockerfile, base];
    }),
  );
  const current = parseKeyValues(
    remote(
      `
state=${q(stateDir)}
test -f "$state/compose.env" || { echo "missing $state/compose.env; install the deployment first" >&2; exit 1; }
release=$(sed -n 's/^CLOUD_AGENTS_RELEASE_DIR=//p' "$state/compose.env" | tail -n 1)
deploy=$(sed -n 's/^CLOUD_AGENTS_DEPLOY_DIR=//p' "$state/compose.env" | tail -n 1)
platform=$(sed -n 's/^CLOUD_AGENTS_PLATFORM=//p' "$state/compose.env" | tail -n 1)
platform=\${platform:-linux/amd64}
arch=\${platform#*/}
echo "platform=$platform"
echo "compose=$(sha256sum "$deploy/compose/docker-compose.yml" 2>/dev/null | cut -d' ' -f1)"
echo "migrations=$(sha256sum "$release"/${q(PLATFORM_RELEASE_MIGRATIONS)} 2>/dev/null | cut -d' ' -f1)"
for base in ${[...new Set(baseImages.values())].map(q).join(" ")}; do
  if docker image inspect "$base" >/dev/null 2>&1; then resolved=$base
  else
    # Hosts without registry access keep a local mirror tagged <digest-prefix>-<arch>.
    digest=$(echo "$base" | sed -n 's/.*@sha256:\\([0-9a-f]\\{8\\}\\).*/\\1/p')
    resolved=$(docker image ls --format '{{.Repository}}:{{.Tag}}' | grep -F ":$digest-$arch" | head -n 1 || true)
  fi
  echo "base:$base=\${resolved:--}"
done
`,
      true,
    ),
  );
  const platform = current.get("platform") ?? "";
  const arch = platform.slice("linux/".length);
  if (platform !== `linux/${arch}` || !["amd64", "arm64"].includes(arch)) {
    fail(`unsupported CLOUD_AGENTS_PLATFORM ${platform}`);
  }
  const resolvedBases = new Map<string, string>();
  for (const base of baseImages.values()) {
    const resolved = current.get(`base:${base}`) ?? "-";
    if (resolved === "-") {
      fail(
        `base image ${base} is not on the host, which has no registry access; load it there or tag a local mirror as <name>:${base.match(/sha256:([0-9a-f]{8})/u)?.[1] ?? "<digest>"}-${arch}`,
      );
    }
    resolvedBases.set(base, resolved);
  }

  // 2. Build the release inputs locally.
  const releaseDir = join(stage, releaseName);
  const deployDir = join(stage, "deployment/deploy");
  mkdirSync(releaseDir, { recursive: true });
  // Image tags digest the binaries, so another toolchain would rebuild every Go service.
  const goVersion = spawnSync(platformGoBinary(), ["version"], { encoding: "utf8" });
  if (!goVersion.stdout?.startsWith(`go version ${PLATFORM_GO_TOOLCHAIN} `)) {
    fail(
      `expected ${PLATFORM_GO_TOOLCHAIN}, found ${goVersion.stdout?.trim() || String(goVersion.error ?? "no go")}; activate the repository mise toolchain`,
    );
  }
  step("Building Web applications");
  for (const directory of ["sdk/typescript", "apps/admin-web", "apps/user-web"]) {
    run("bun", ["run", "--cwd", directory, "build"]);
  }
  step(`Building Go services for ${platform}`);
  for (const command of goCommands) {
    const build = platformGoBuildCommand(
      repositoryRoot,
      command,
      `linux-${arch}` as PlatformReleaseTarget,
      join(releaseDir, `${command}-linux-${arch}`),
    );
    run(build.command, build.args, build.env);
  }
  step("Packaging deployment and migrations");
  writeFileSync(
    join(releaseDir, PLATFORM_RELEASE_DEPLOYMENT),
    buildPlatformDeploymentPackage(repositoryRoot, buildWorkerOciSupplyArtifacts(repositoryRoot)),
  );
  const migrations = buildPlatformMigrationPackage(repositoryRoot);
  writeFileSync(join(releaseDir, PLATFORM_RELEASE_MIGRATIONS), migrations);
  mkdirSync(join(stage, "deployment"));
  run("tar", [
    "-xf",
    join(releaseDir, PLATFORM_RELEASE_DEPLOYMENT),
    "-C",
    join(stage, "deployment"),
  ]);
  writeFileSync(
    join(releaseDir, "checksums.sha256"),
    readdirSync(releaseDir)
      .toSorted()
      .map((name) => `${sha256(readFileSync(join(releaseDir, name)))}  ${name}\n`)
      .join(""),
  );

  // 3. Decide what changed.
  const plan = images.map((image) => {
    const base = resolvedBases.get(baseImages.get(image.dockerfile)!)!;
    const contextDir = image.context === "release" ? releaseDir : deployDir;
    const hash = createHash("sha256").update(`${platform}\0${base}\0`);
    const dockerfile = join(deployDir, "docker", image.dockerfile);
    hash.update(`${image.dockerfile}\0`).update(readFileSync(dockerfile));
    for (const file of image.inputs(arch).flatMap((input) => files(join(contextDir, input)))) {
      hash.update(`${relative(contextDir, file)}\0`).update(readFileSync(file));
    }
    return { image, base, tag: `dev-${hash.digest("hex").slice(0, 16)}` };
  });
  const existing = new Set(
    remote(
      plan
        .map(
          ({ image, tag }) =>
            `if docker image inspect cloud-agents-${image.service}:${tag} >/dev/null 2>&1; then echo ${image.service}; fi`,
        )
        .join("\n"),
      true,
    )
      .split("\n")
      .filter(Boolean),
  );
  const changed = plan.filter(({ image }) => rebuildAll || !existing.has(image.service));
  const composeChanged =
    sha256(readFileSync(join(deployDir, "compose/docker-compose.yml"))) !== current.get("compose");
  const migrationsChanged = sha256(migrations) !== current.get("migrations");

  step("Plan");
  for (const { image, tag } of plan) {
    const action = changed.some((entry) => entry.image === image) ? "rebuild" : "unchanged";
    process.stdout.write(`  ${image.service.padEnd(16)}${action.padEnd(11)}${tag}\n`);
  }
  process.stdout.write(
    `  ${"compose file".padEnd(16)}${composeChanged ? "changed" : "unchanged"}\n`,
  );
  process.stdout.write(
    `  ${"migrations".padEnd(16)}${migrationsChanged ? "changed (database is backed up first)" : "unchanged"}\n`,
  );
  if (changed.length === 0 && !composeChanged && !migrationsChanged) {
    step("Already up to date");
    return;
  }
  if (args.has("--dry-run")) {
    step("Dry run: nothing was uploaded or changed");
    return;
  }
  if (migrationsChanged && !args.has("--yes")) await confirm("Apply new database migrations?");

  // 4. Upload and verify the release, then unpack its deployment package.
  step(`Uploading ${releaseName}`);
  const unpack = [
    "set -eu",
    `cd ${q(remoteRoot)}`,
    `test ! -e ${q(releaseName)} && test ! -e ${q(deploymentName)}`,
    "tar -xf -",
    `(cd ${q(releaseName)} && sha256sum -c --quiet checksums.sha256)`,
    `mkdir ${q(deploymentName)}`,
    `tar -xf ${q(`${releaseName}/${PLATFORM_RELEASE_DEPLOYMENT}`)} -C ${q(deploymentName)}`,
  ].join("; ");
  check(
    "upload",
    spawnSync(
      "sh",
      [
        "-c",
        `set -eu; tar -C ${q(stage)} -cf - ${q(releaseName)} | ssh ${[...sshOptions, target].map(q).join(" ")} ${q(unpack)}`,
      ],
      { stdio: "inherit" },
    ),
  );

  // 5. Build only the changed images on the host.
  const newRelease = `${remoteRoot}/${releaseName}`;
  const newDeploy = `${remoteRoot}/${deploymentName}/deploy`;
  if (changed.length > 0) {
    step(`Building images: ${changed.map(({ image }) => image.service).join(", ")}`);
    remote(
      changed
        .flatMap(({ image, base, tag }) => {
          const name = `cloud-agents-${image.service}:${tag}`;
          const baseArg =
            base === baseImages.get(image.dockerfile) ? "" : ` --build-arg BASE_IMAGE=${q(base)}`;
          const context = image.context === "release" ? newRelease : newDeploy;
          return [
            `docker build --platform ${q(platform)}${baseArg} -f ${q(`${newDeploy}/docker/${image.dockerfile}`)} -t ${q(name)} ${q(context)}`,
            ...image.aliases.map(
              (alias) => `docker tag ${q(name)} ${q(`cloud-agents-${alias}:${tag}`)}`,
            ),
          ];
        })
        .join("\n"),
    );
  }

  // 6. Back up the database before new migrations run.
  if (migrationsChanged) {
    const backup = `${remoteRoot}/backup-pre-${stamp}`;
    step(`Backing up database and state files to ${backup}`);
    remote(`
umask 077
mkdir -p ${q(`${backup}/state-files`)}
cd ${q(stateDir)}
cp -p ${[...stateFiles, "compose.override.yml"].join(" ")} ${q(`${backup}/state-files/`)}
./compose.sh --profile backup run --rm -T backup > ${q(`${backup}/database.dump.tmp`)}
mv ${q(`${backup}/database.dump.tmp`)} ${q(`${backup}/database.dump`)}
`);
  }

  // 7. Validate the new Compose configuration, switch to it and recreate changed services.
  step("Switching deployment");
  const imagesYaml = `services:\n${plan
    .flatMap(({ image, tag }) =>
      [image.service, ...image.aliases].map(
        (service) => `  ${service}:\n    image: cloud-agents-${service}:${tag}\n`,
      ),
    )
    .join("")}`;
  const composeFile = `${newDeploy}/compose/docker-compose.yml`;
  const composeScript = `#!/bin/sh\nexec docker compose --env-file ${stateDir}/compose.env -f ${composeFile} -f ${stateDir}/compose.override.yml -f ${stateDir}/compose.images.yml "$@"\n`;
  remote(`
umask 077
cd ${q(stateDir)}
awk -v release=${q(newRelease)} -v deploy=${q(newDeploy)} '
  /^CLOUD_AGENTS_RELEASE_DIR=/ { print "CLOUD_AGENTS_RELEASE_DIR=" release; next }
  /^CLOUD_AGENTS_DEPLOY_DIR=/ { print "CLOUD_AGENTS_DEPLOY_DIR=" deploy; next }
  { print }' compose.env > compose.env.next
test "$(grep -c -e '^CLOUD_AGENTS_RELEASE_DIR=' -e '^CLOUD_AGENTS_DEPLOY_DIR=' compose.env.next)" = 2
printf '%s' ${q(imagesYaml)} > compose.images.yml.next
printf '%s' ${q(composeScript)} > compose.sh.next
chmod 0644 compose.images.yml.next
chmod 0755 compose.sh.next
docker compose --env-file compose.env.next -f ${q(composeFile)} -f compose.override.yml -f compose.images.yml.next config -q
for name in ${stateFiles.join(" ")}; do
  cp -p "$name" "$name.bak-${stamp}"
  mv "$name.next" "$name"
done
./compose.sh up -d --no-build${rebuildAll ? " --force-recreate" : ""}
`);

  // 8. Require long-running services to stay up without restarts for ~10 seconds,
  // so a container that crashes shortly after start is not reported as deployed.
  step("Checking services");
  const rollback = `cd ${stateDir} && for f in ${stateFiles.join(" ")}; do cp -p "$f.bak-${stamp}" "$f"; done && ./compose.sh up -d --no-build`;
  remote(`
cd ${q(stateDir)}
restarts() { docker inspect -f '{{.RestartCount}}' $(./compose.sh ps -q "$1") 2>/dev/null || echo missing; }
baseline=""
for service in ${runningServices.join(" ")}; do baseline="$baseline $service=$(restarts "$service")"; done
stable=0
for attempt in $(seq 1 45); do
  bad=$(./compose.sh ps -a --format '{{.Service}} {{.State}}' | awk '$1 ~ /^(${runningServices.join("|")})$/ && $2 != "running" { print $1 }')
  for service in ${runningServices.join(" ")}; do
    case " $baseline " in *" $service=$(restarts "$service") "*) ;; *) bad="$bad $service" ;; esac
  done
  bad=$(echo $bad | tr ' ' '\\n' | sort -u | tr '\\n' ' ' | sed 's/ $//')
  if [ -z "$bad" ]; then stable=$((stable + 1)); else stable=0; fi
  if [ "$stable" -ge 5 ]; then break; fi
  sleep 2
done
./compose.sh ps --format 'table {{.Service}}\\t{{.Image}}\\t{{.Status}}'
if [ -n "$bad" ]; then
  for service in $bad; do echo "--- $service"; ./compose.sh logs --tail 40 "$service"; done
  echo "Services not running: $bad" >&2
  echo ${q(`Roll back with: ${rollback}`)} >&2
  exit 1
fi
`);
  step(`Deployed ${stamp}`);
  process.stdout.write(`Roll back with: ${rollback}\n`);
}

function remote(script: string, capture = false): string {
  const result = spawnSync("ssh", [...sshOptions, target, "sh -s"], {
    input: `set -eu\n${script}`,
    encoding: "utf8",
    stdio: ["pipe", capture ? "pipe" : "inherit", "inherit"],
  });
  check("remote command", result);
  return result.stdout ?? "";
}

function run(command: string, commandArgs: readonly string[], env: Record<string, string> = {}) {
  const result = spawnSync(command, [...commandArgs], {
    cwd: repositoryRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "inherit"],
    env: { ...process.env, ...env },
  });
  if (result.status !== 0) process.stdout.write(result.stdout ?? "");
  check(`${command} ${commandArgs.join(" ")}`, result);
}

function check(label: string, result: { error?: Error; status: number | null }): void {
  if (result.error) throw result.error;
  if (result.status !== 0) fail(`${label} exited with status ${String(result.status)}`);
}

function parseKeyValues(text: string): Map<string, string> {
  return new Map(
    text
      .split("\n")
      .filter((line) => line.includes("="))
      .map((line) => {
        const separator = line.lastIndexOf("=");
        return [line.slice(0, separator), line.slice(separator + 1)];
      }),
  );
}

function files(path: string): string[] {
  if (!statSync(path).isDirectory()) return [path];
  return readdirSync(path, { recursive: true, encoding: "utf8" })
    .map((name) => join(path, name))
    .filter((file) => statSync(file).isFile())
    .toSorted();
}

function sha256(bytes: Uint8Array): string {
  return createHash("sha256").update(bytes).digest("hex");
}

async function confirm(question: string): Promise<void> {
  if (!process.stdin.isTTY) fail(`${question} Re-run with --yes to confirm.`);
  const prompt = createInterface({ input: process.stdin, output: process.stdout });
  const answer = await prompt.question(`${question} [y/N] `);
  prompt.close();
  if (!/^y(es)?$/iu.test(answer.trim())) fail("cancelled");
}

function q(value: string): string {
  return `'${value.replaceAll("'", `'\\''`)}'`;
}

function step(message: string): void {
  process.stdout.write(`\n==> ${message}\n`);
}

function fail(message: string): never {
  throw new DeployError(message);
}
