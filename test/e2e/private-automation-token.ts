import { closeSync, constants, fstatSync, lstatSync, openSync, readFileSync } from "node:fs";

const invalidTokenFile = "automation token file invalid";
const jwt = /^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/u;
const maxTokenFileBytes = 8 * 1024;

export function readPrivateAutomationToken(path: string): string {
  let descriptor: number | undefined;
  try {
    const pathStat = lstatSync(path, { bigint: true });
    if (
      !pathStat.isFile() ||
      pathStat.isSymbolicLink() ||
      (pathStat.mode & 0o077n) !== 0n ||
      pathStat.size < 1n ||
      pathStat.size > BigInt(maxTokenFileBytes)
    )
      throw new Error(invalidTokenFile);

    descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    const opened = fstatSync(descriptor, { bigint: true });
    if (
      !opened.isFile() ||
      opened.dev !== pathStat.dev ||
      opened.ino !== pathStat.ino ||
      opened.size !== pathStat.size ||
      (opened.mode & 0o077n) !== 0n
    )
      throw new Error(invalidTokenFile);

    const raw = readFileSync(descriptor, "utf8");
    const after = fstatSync(descriptor, { bigint: true });
    if (
      after.dev !== opened.dev ||
      after.ino !== opened.ino ||
      after.size !== opened.size ||
      after.mtimeNs !== opened.mtimeNs ||
      after.ctimeNs !== opened.ctimeNs
    )
      throw new Error(invalidTokenFile);

    const token = raw.endsWith("\n") ? raw.slice(0, -1) : raw;
    if (!jwt.test(token)) throw new Error(invalidTokenFile);
    return token;
  } catch {
    throw new Error(invalidTokenFile);
  } finally {
    if (descriptor !== undefined) closeSync(descriptor);
  }
}
