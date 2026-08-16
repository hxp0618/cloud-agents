import { describe, expect, it } from "vitest";

import {
  parseGoModule,
  validateGoModule,
  validateGoModuleEdit,
  validateGoSourceImports,
} from "./platform-go-modules";

const VALID_MODULE = `module github.com/hxp0618/cloud-agents/sdk/go

go 1.26.0

toolchain go1.26.6
`;

describe("Platform Go module boundaries", () => {
  it("accepts the exact toolchain without local replacements", () => {
    expect(parseGoModule(VALID_MODULE)).toEqual({
      module: "github.com/hxp0618/cloud-agents/sdk/go",
      goVersion: "1.26.0",
      toolchain: "go1.26.6",
      hasReplace: false,
    });
    expect(() =>
      validateGoModule(VALID_MODULE, "github.com/hxp0618/cloud-agents/sdk/go"),
    ).not.toThrow();
  });

  it("semantically rejects go mod edit replace entries", () => {
    const document = {
      Module: { Path: "github.com/hxp0618/cloud-agents/sdk/go" },
      Go: "1.26.0",
      Toolchain: "go1.26.6",
      Replace: [{ Old: { Path: "example.invalid/sdk" }, New: { Path: "../../sdk" } }],
    };
    expect(() =>
      validateGoModuleEdit(document, "github.com/hxp0618/cloud-agents/sdk/go", "sdk/go/go.mod"),
    ).toThrow(/replace directives/);
  });

  it.each([
    ["wrong module", VALID_MODULE.replace("sdk/go", "services/worker"), /must be/],
    ["floating Go line", VALID_MODULE.replace("go 1.26.0", "go 1.26"), /go directive/],
    ["wrong patch toolchain", VALID_MODULE.replace("go1.26.6", "go1.26.5"), /toolchain/],
    ["local replace", `${VALID_MODULE}\nreplace example.invalid/sdk => ../../sdk\n`, /replace/],
  ])("rejects %s", (_name, source, expected) => {
    expect(() => validateGoModule(source, "github.com/hxp0618/cloud-agents/sdk/go")).toThrow(
      expected,
    );
  });

  it("rejects SDK-to-service and service-to-service imports", () => {
    expect(() =>
      validateGoSourceImports(
        'package sdk\nimport "github.com/hxp0618/cloud-agents/services/worker/internal/job"\n',
        "sdk/go/generated.go",
        ["github.com/hxp0618/cloud-agents/services/"],
      ),
    ).toThrow(/forbidden module boundary/);
    expect(() =>
      validateGoSourceImports(
        `package controlplane
import (
  sdk "github.com/hxp0618/cloud-agents/sdk/go"
  "github.com/hxp0618/cloud-agents/services/worker"
)
`,
        "services/control-plane/service.go",
        ["github.com/hxp0618/cloud-agents/services/worker"],
      ),
    ).toThrow(/services\/worker/);
  }, 30_000);

  it.each([
    [
      "raw string",
      "package sdk\nimport `github.com/hxp0618/cloud-agents/services/worker/publicapi`\n",
    ],
    [
      "raw string block",
      "package sdk\nimport (\n  `github.com/hxp0618/cloud-agents/services/worker/publicapi`\n)\n",
    ],
    [
      "comment-separated import",
      'package sdk\nimport /* boundary bypass */ "github.com/hxp0618/cloud-agents/services/worker/publicapi"\n',
    ],
    [
      "Unicode alias",
      'package sdk\nimport 工作器 "github.com/hxp0618/cloud-agents/services/worker/publicapi"\n',
    ],
  ])("rejects a forbidden %s import through the Go parser", (_name, source) => {
    expect(() =>
      validateGoSourceImports(source, "sdk/go/bypass.go", [
        "github.com/hxp0618/cloud-agents/services/",
      ]),
    ).toThrow(/forbidden module boundary/);
  });

  it("fails closed on malformed Go source", () => {
    expect(() =>
      validateGoSourceImports('package sdk\nimport "unterminated\n', "sdk/go/broken.go", [
        "github.com/hxp0618/cloud-agents/services/",
      ]),
    ).toThrow(/Go import parser failed/);
  });

  it("allows both services to consume the public SDK", () => {
    expect(() =>
      validateGoSourceImports(
        'package controlplane\nimport sdk "github.com/hxp0618/cloud-agents/sdk/go"\n',
        "services/control-plane/service.go",
        ["github.com/hxp0618/cloud-agents/services/worker"],
      ),
    ).not.toThrow();
  });
});
