import { describe, expect, it } from "vitest";

import { parseEmailDomains } from "../../src/app/auth";

describe("email domain policy input", () => {
  it("normalizes IDNA, removes duplicates, and returns canonical order", () => {
    expect(parseEmailDomains("Identity.TEST, bücher.example\nEXAMPLE.test identity.test")).toEqual([
      "example.test",
      "identity.test",
      "xn--bcher-kva.example",
    ]);
  });

  it.each(["*.example.test", "example.test/path", "example.test:443", "single-label"])(
    "rejects a non-domain value: %s",
    (value) => {
      expect(parseEmailDomains(value)).toBeNull();
    },
  );

  it("rejects more domains than the contract permits", () => {
    expect(
      parseEmailDomains(Array.from({ length: 65 }, (_, index) => `d${index}.example`).join("\n")),
    ).toBeNull();
  });
});
