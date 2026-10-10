import { describe, expect, it } from "vitest";

import {
  identifierFromName,
  identifierWithSuffix,
  nextProfileVersion,
  uniqueIdentifier,
} from "../../src/admin/identifiers";
import { targetIdentifierPattern } from "../../src/admin/filters";

const identifier = new RegExp(`^${targetIdentifierPattern}$`, "u");

describe("derived resource identifiers", () => {
  it.each([
    ["Docker-Primary", "docker-primary"],
    ["  SSH Overflow  ", "ssh-overflow"],
    ["Café  Résumé", "cafe-resume"],
    ["-_.edge.~", "edge"],
    ["飞书", "feishu"],
    [`${"a".repeat(127)}.b`, "a".repeat(127)],
  ])("derives a contract identifier from %j", (name, expected) => {
    const value = identifierFromName(name, "feishu");
    expect(value).toBe(expected);
    expect(value).toMatch(identifier);
  });

  it("keeps suffixed and de-duplicated identifiers within the contract", () => {
    const long = "w".repeat(128);
    expect(identifierWithSuffix("worker-a", "1a2b3c4d")).toBe("worker-a-1a2b3c4d");
    expect(identifierWithSuffix(long, "1a2b3c4d")).toMatch(identifier);
    expect(identifierWithSuffix(long, "1a2b3c4d")).toHaveLength(128);
    expect(uniqueIdentifier("github", new Set(["github", "github-2"]))).toBe("github-3");
    expect(uniqueIdentifier(long, new Set([long]))).toMatch(identifier);
  });

  it("continues the consecutive version of the same profile only", () => {
    const profiles = [
      { spec: { profileId: "dev", version: 1 } },
      { spec: { profileId: "dev", version: 2 } },
      { spec: { profileId: "prod", version: 7 } },
    ];
    expect(nextProfileVersion(profiles, "dev")).toBe(3);
    expect(nextProfileVersion(profiles, "new")).toBe(1);
  });
});
