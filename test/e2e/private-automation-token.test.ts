import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { afterEach, test } from "node:test";
import { readPrivateAutomationToken } from "./private-automation-token";

const directories: string[] = [];
const token = "header.payload.signature";

function temporaryDirectory(): string {
  const directory = mkdtempSync(join(tmpdir(), "cloud-agents-automation-token-"));
  directories.push(directory);
  return directory;
}

afterEach(() => {
  for (const directory of directories.splice(0))
    rmSync(directory, { recursive: true, force: true });
});

test("reads only a bounded private regular automation token file", () => {
  const directory = temporaryDirectory();
  const path = join(directory, "token");
  writeFileSync(path, `${token}\n`, { mode: 0o600 });
  assert.equal(readPrivateAutomationToken(path), token);

  chmodSync(path, 0o644);
  assert.throws(() => readPrivateAutomationToken(path), {
    message: "automation token file invalid",
  });
});

test("rejects links and malformed tokens without echoing file content", () => {
  const directory = temporaryDirectory();
  const malformed = join(directory, "malformed");
  const link = join(directory, "link");
  writeFileSync(malformed, "do-not-echo-this-value\n", { mode: 0o600 });
  symlinkSync(malformed, link);

  for (const path of [malformed, link]) {
    let message = "";
    try {
      readPrivateAutomationToken(path);
    } catch (error) {
      message = error instanceof Error ? error.message : String(error);
    }
    assert.equal(message, "automation token file invalid");
    assert.equal(message.includes("do-not-echo-this-value"), false);
  }
});
