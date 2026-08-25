import Ajv2020 from "ajv/dist/2020.js";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

const repositoryRoot = resolve(import.meta.dirname, "../..");
const schemaPath = resolve(
  repositoryRoot,
  "tools/generator-supply/v3/generator-supply-profile-source-v3.schema.json",
);

describe("generator-supply source v3 schema", () => {
  it("compiles with Ajv 2020 strict type checking", () => {
    const schema = JSON.parse(readFileSync(schemaPath, "utf8")) as object;
    const ajv = new Ajv2020({ allErrors: true, strict: true, validateFormats: false });

    expect(() => ajv.compile(schema)).not.toThrow();
  });
});
