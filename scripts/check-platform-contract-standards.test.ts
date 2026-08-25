import { describe, expect, it } from "vitest";

import {
  buildContractStandardsPythonArguments,
  buildUvPipSyncArguments,
} from "./check-platform-contract-standards";

describe("contract standards uv pip sync arguments", () => {
  it("explicitly selects the current versioned v3 profile", () => {
    expect(buildContractStandardsPythonArguments()).toEqual([
      "-B",
      "tools/contract-standards/check_contract_standards.py",
      "--root",
      ".",
      "--profile",
      "tools/contract-standards/profile-v3.json",
    ]);
  });

  it("makes the full requirements sync strictly offline when a wheelhouse is set", () => {
    const wheelhouse = "/tmp/contract standards wheelhouse";

    expect(buildUvPipSyncArguments("/tmp/venv/bin/python", wheelhouse)).toEqual([
      "pip",
      "sync",
      "--python",
      "/tmp/venv/bin/python",
      "--require-hashes",
      "--no-build",
      "--strict",
      "--no-index",
      "--find-links",
      wheelhouse,
      "-",
    ]);
  });

  it("keeps the existing online sync behavior when no wheelhouse is set", () => {
    const arguments_ = buildUvPipSyncArguments("/tmp/venv/bin/python", undefined);

    expect(arguments_).toEqual([
      "pip",
      "sync",
      "--python",
      "/tmp/venv/bin/python",
      "--require-hashes",
      "--no-build",
      "--strict",
      "-",
    ]);
    expect(arguments_).not.toContain("--no-index");
    expect(arguments_).not.toContain("--find-links");
  });
});
