import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

import { classifyMigrationStatement, splitPostgresStatements } from "./platform-migration-sql";

const root = resolve(import.meta.dirname, "../..");
const predecessorPath = resolve(
  root,
  "services/control-plane/migrations/000013_add_durable_project_create_writer.sql",
);
const successorPath = resolve(
  root,
  "services/control-plane/migrations/000014_harden_durable_project_create_identifiers.sql",
);

describe("durable Project-create identifier successor", () => {
  it("keeps 000013 historical MD5 and confines SHA-256 to 000014", () => {
    const predecessor = readFileSync(predecessorPath, "utf8");
    const successor = readFileSync(successorPath, "utf8");

    expect(predecessor).toContain("pg_catalog.md5(");
    expect(createHash("sha256").update(predecessor).digest("hex")).toBe(
      "d8c3687e300767f7e27f673c6a9fc3de098fbec1b8911dc018c47d32de33dffa",
    );
    expect(successor.toLowerCase()).not.toContain("md5(");
    expect(successor).toContain(
      "CREATE OR REPLACE FUNCTION cloud_agents.create_managed_agent_project_durable_v1",
    );
    const statements = splitPostgresStatements(new TextEncoder().encode(successor));
    expect(statements).toHaveLength(1);
    expect(classifyMigrationStatement(statements[0]!, "000014")).toEqual({
      profile: "postgresql-ddl-v1",
      command: "CREATE",
      object_kind: "FUNCTION",
      target_identity:
        "function:unquoted:cloud_agents/unquoted:create_managed_agent_project_durable_v1(unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text)",
      grantee: null,
      special_case: null,
    });
  });

  it("uses two domain-separated framed SHA-256 identifiers with full-width bounds", () => {
    const successor = readFileSync(successorPath, "utf8");
    const operationDomain = "cloud-agents/durable-project-create/operation-id/v1";
    const eventDomain = "cloud-agents/durable-project-create/event-id/v1";
    expect(successor).toContain(operationDomain);
    expect(successor).toContain(eventDomain);
    expect(successor.match(/pg_catalog\.sha256\(/gu)).toHaveLength(2);
    expect(successor.match(/pg_catalog\.encode\(/gu)).toHaveLength(2);
    expect(successor.match(/pg_catalog\.convert_to\(/gu)).toHaveLength(2);
    expect(successor.match(/'UTF8'/gu)).toHaveLength(2);
    expect(successor.match(/pg_catalog\.octet_length\(/gu)!).toHaveLength(8);
    expect(successor).toContain("operation_suffix text");
    expect(successor).toContain("event_suffix text");
    expect(successor).toContain("derived_operation_id := 'project-create-' || operation_suffix");
    expect(successor).toContain("derived_event_id := 'project-create-event-' || event_suffix");
    expect(successor).not.toContain(
      "derived_event_id := 'project-create-event-' || operation_suffix",
    );
    expect("project-create-".length + 64).toBe(79);
    expect("project-create-event-".length + 64).toBe(85);
    expect(successor).toContain("'hex'");

    const subject = `sha256:${"a".repeat(64)}`;
    const idempotencyKey = "key-012345678901";
    const request = `sha256:${"b".repeat(64)}`;
    const frame = (domain: string): string =>
      `${Buffer.byteLength(domain)}:${domain}${Buffer.byteLength(subject)}:${subject}${Buffer.byteLength(idempotencyKey)}:${idempotencyKey}${Buffer.byteLength(request)}:${request}`;
    const operationDigest = createHash("sha256")
      .update(frame(operationDomain), "utf8")
      .digest("hex");
    const eventDigest = createHash("sha256").update(frame(eventDomain), "utf8").digest("hex");
    expect(operationDigest).toBe(
      "c9896e5270a50c9ae68ff7bc99a65e63392299aa2c82b637ee6f60e7697bbb3c",
    );
    expect(eventDigest).toBe("2c86a14110b5109dda4366edee0cbcbd2b8b26c1d8cf6e3710a8a994841465b9");
    expect(operationDigest).not.toBe(eventDigest);
  });
});
