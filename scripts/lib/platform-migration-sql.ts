import { createHash } from "node:crypto";

import { MigrationValidationError } from "./platform-migration-json";

export type SqlStatementSlice = {
  readonly index: number;
  readonly start: number;
  readonly end: number;
  readonly bytes: Uint8Array;
  readonly sha256: string;
};

export type SqlStatementClassification = {
  readonly profile: "postgresql-ddl-v1";
  readonly command: string;
  readonly object_kind: string;
  readonly target_identity: string;
  readonly grantee: string | null;
  readonly special_case: string | null;
};

const ASCII = new TextDecoder("ascii");
const ALLOWED_GRANTEES = new Set([
  "PUBLIC",
  "CLOUD_AGENTS_RUNTIME",
  "CLOUD_AGENTS_BOOTSTRAP_ADMIN",
]);
const INITIAL_DO_SHA256 = "sha256:4cce367246af1fe1e08191df7d48bf8b9dad7ee2696b754f6c2df9f66c559281";
const EXACT_INSERT_SPECIAL_CASES: ReadonlyMap<
  string,
  {
    readonly migrationId: string;
    readonly statementIndex: number;
    readonly targetIdentity: string;
  }
> = new Map([
  [
    "sha256:004150417e326e671f4a8aa198ab9c8f955dedfa21966f3525b9ddf451d393be",
    {
      migrationId: "000003",
      statementIndex: 44,
      targetIdentity: "table:unquoted:cloud_agents/unquoted:builtin_roles",
    },
  ],
  [
    "sha256:0e9974a61b7e24895ab1c824c89b35c74d52bf6b49b51b0d675134eb7796b8a8",
    {
      migrationId: "000003",
      statementIndex: 45,
      targetIdentity: "table:unquoted:cloud_agents/unquoted:builtin_role_permissions",
    },
  ],
]);

export function splitPostgresStatements(input: Uint8Array): ReadonlyArray<SqlStatementSlice> {
  const statements: SqlStatementSlice[] = [];
  let start = 0;
  let index = 0;
  let blockDepth = 0;
  let lineComment = false;
  let singleQuote = false;
  let singleQuoteEscapes = false;
  let doubleQuote = false;
  let dollarTag: Uint8Array | undefined;
  for (let offset = 0; offset < input.length; offset += 1) {
    const byte = input[offset]!;
    const next = input[offset + 1];
    if (lineComment) {
      if (byte === 0x0a) lineComment = false;
      continue;
    }
    if (blockDepth > 0) {
      if (byte === 0x2f && next === 0x2a) {
        blockDepth += 1;
        offset += 1;
      } else if (byte === 0x2a && next === 0x2f) {
        blockDepth -= 1;
        offset += 1;
      }
      continue;
    }
    if (dollarTag) {
      if (matchesAt(input, dollarTag, offset)) {
        offset += dollarTag.length - 1;
        dollarTag = undefined;
      }
      continue;
    }
    if (singleQuote) {
      if (byte === 0x27 && next === 0x27) {
        offset += 1;
      } else if (singleQuoteEscapes && byte === 0x5c && next !== undefined) {
        offset += 1;
      } else if (byte === 0x27) {
        singleQuote = false;
      }
      continue;
    }
    if (doubleQuote) {
      if (byte === 0x22 && next === 0x22) offset += 1;
      else if (byte === 0x22) doubleQuote = false;
      continue;
    }
    if (byte === 0x2d && next === 0x2d) {
      lineComment = true;
      offset += 1;
      continue;
    }
    if (byte === 0x2f && next === 0x2a) {
      blockDepth = 1;
      offset += 1;
      continue;
    }
    if (byte === 0x27) {
      singleQuoteEscapes = isEscapeStringQuote(input, offset);
      singleQuote = true;
      continue;
    }
    if (byte === 0x22) {
      doubleQuote = true;
      continue;
    }
    if (byte === 0x24) {
      const tag = readDollarTag(input, offset);
      if (tag) {
        dollarTag = tag;
        offset += tag.length - 1;
        continue;
      }
    }
    if (byte === 0x3b) {
      const bytes = input.slice(start, offset + 1);
      if (!containsSqlToken(input.slice(start, offset))) {
        throw new MigrationValidationError("EMPTY_SQL_STATEMENT", `statement ${index}`);
      }
      statements.push({
        index,
        start,
        end: offset + 1,
        bytes,
        sha256: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
      });
      index += 1;
      start = offset + 1;
    }
  }
  if (blockDepth !== 0 || lineComment || singleQuote || doubleQuote || dollarTag) {
    if (blockDepth !== 0 || singleQuote || doubleQuote || dollarTag) {
      throw new MigrationValidationError(
        "UNTERMINATED_SQL_LEXEME",
        "unterminated SQL lexical state",
      );
    }
  }
  if (containsSqlToken(input.slice(start))) {
    throw new MigrationValidationError("SQL_TERMINATOR_REQUIRED", `offset ${start}`);
  }
  return statements;
}

export function classifyMigrationStatement(
  statement: SqlStatementSlice,
  migrationId: string,
): SqlStatementClassification {
  const tokens = lexTopLevelTokens(statement.bytes);
  const first = tokens[0];
  if (!first) throw new MigrationValidationError("EMPTY_SQL_STATEMENT", String(statement.index));
  if (first === "DO") {
    if (
      migrationId !== "000001" ||
      statement.index !== 0 ||
      statement.sha256 !== INITIAL_DO_SHA256
    ) {
      throw new MigrationValidationError("SQL_DO_SPECIAL_CASE_MISMATCH", statement.sha256);
    }
    return {
      profile: "postgresql-ddl-v1",
      command: "DO",
      object_kind: "SCHEMA_BOOTSTRAP",
      target_identity: "schema:unquoted:cloud_agents",
      grantee: null,
      special_case: `${migrationId}:${statement.index}:${statement.sha256}`,
    };
  }
  if (first === "CREATE") {
    const orReplace = tokens[1] === "OR" && tokens[2] === "REPLACE";
    if (
      orReplace &&
      (!new Set(["000005", "000006"]).has(migrationId) || tokens[3] !== "FUNCTION")
    ) {
      reject(tokens);
    }
    const kindOffset = orReplace ? 3 : 1;
    const targetOffset = orReplace ? 4 : 2;
    const kind = tokens[kindOffset];
    if (!kind || !new Set(["TABLE", "INDEX", "POLICY", "FUNCTION"]).has(kind)) {
      reject(tokens);
    }
    let targetIdentity: string;
    if (kind === "TABLE" || kind === "FUNCTION") {
      requireCloudAgentsQualified(tokens, targetOffset);
      if (kind === "TABLE") {
        const closing = matchingCloseParenthesis(tokens, targetOffset + 3);
        if (orReplace || closing !== tokens.length - 2) reject(tokens);
        targetIdentity = qualifiedIdentity("table", tokens, targetOffset);
      } else {
        const signatureEnd = matchingCloseParenthesis(tokens, targetOffset + 3);
        const body = tokens.lastIndexOf("$BODY$");
        const options = tokens.slice(signatureEnd + 1, body - 1);
        if (
          signatureEnd < 0 ||
          body <= signatureEnd ||
          body !== tokens.length - 2 ||
          tokens[body - 1] !== "AS" ||
          options.some((token) =>
            new Set([
              "TABLESPACE",
              "WITH",
              "EXTRA",
              "OWNER",
              "DROP",
              "ALTER",
              "CREATE",
              "GRANT",
              "REVOKE",
              ";",
            ]).has(token),
          )
        )
          reject(tokens);
        targetIdentity = qualifiedIdentity("function", tokens, targetOffset, signatureEnd);
      }
    } else if (kind === "INDEX") {
      const on = tokens.indexOf("ON");
      if (on !== 3) reject(tokens);
      requireCloudAgentsQualified(tokens, on + 1);
      const closing = matchingCloseParenthesis(tokens, on + 4);
      if (closing !== tokens.length - 2) reject(tokens);
      targetIdentity = qualifiedDerivedIdentity("index", tokens, on + 1, tokens[2]!);
    } else if (kind === "POLICY") {
      const on = tokens.indexOf("ON");
      if (on !== 3) reject(tokens);
      requireCloudAgentsQualified(tokens, on + 1);
      validateCreatePolicyTail(tokens, on + 4);
      targetIdentity = qualifiedDerivedIdentity("policy", tokens, on + 1, tokens[2]!);
    } else {
      reject(tokens);
    }
    if (orReplace) {
      const expectedReplacement = new Map([
        [
          "000005",
          "function:unquoted:cloud_agents/unquoted:bind_role(unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
        ],
        [
          "000006",
          "function:unquoted:cloud_agents/unquoted:subject_ref_digest(unquoted:text,unquoted:text,unquoted:text)",
        ],
      ]).get(migrationId);
      if (targetIdentity !== expectedReplacement) reject(tokens);
    }
    return classification("CREATE", kind!, targetIdentity!, null);
  }
  if (first === "ALTER") {
    const kind = tokens[1];
    if (kind === "TABLE") {
      requireCloudAgentsQualified(tokens, 2);
      const subcommand = tokens.slice(5, -1);
      const exact = [
        ["OWNER", "TO", "CLOUD_AGENTS_MIGRATION_OWNER"],
        ["ENABLE", "ROW", "LEVEL", "SECURITY"],
        ["FORCE", "ROW", "LEVEL", "SECURITY"],
      ].some((candidate) => candidate.join("\0") === subcommand.join("\0"));
      const addConstraint =
        subcommand[0] === "ADD" &&
        subcommand[1] === "CONSTRAINT" &&
        !hasTopLevelComma(subcommand.slice(2));
      const targetIdentity = qualifiedIdentity("table", tokens, 2);
      const dropResourceKindConstraint =
        migrationId === "000003" &&
        targetIdentity === "table:unquoted:cloud_agents/unquoted:resource_changes" &&
        subcommand.join("\0") ===
          ["DROP", "CONSTRAINT", "RESOURCE_CHANGES_RESOURCE_KIND"].join("\0");
      const dropAuditFactConstraint =
        migrationId === "000004" &&
        targetIdentity === "table:unquoted:cloud_agents/unquoted:audit_facts" &&
        new Set([
          ["DROP", "CONSTRAINT", "AUDIT_FACTS_ACTION"].join("\0"),
          ["DROP", "CONSTRAINT", "AUDIT_FACTS_RESOURCE_KIND"].join("\0"),
        ]).has(subcommand.join("\0"));
      if (!exact && !addConstraint && !dropResourceKindConstraint && !dropAuditFactConstraint)
        reject(tokens);
      return classification("ALTER", "TABLE", targetIdentity, null);
    }
    if (kind === "FUNCTION") {
      requireCloudAgentsQualified(tokens, 2);
      const closing = matchingCloseParenthesis(tokens, 5);
      if (
        closing < 0 ||
        tokens.slice(closing + 1).join("\0") !==
          ["OWNER", "TO", "CLOUD_AGENTS_MIGRATION_OWNER", ";"].join("\0")
      ) {
        reject(tokens);
      }
      return classification(
        "ALTER",
        "FUNCTION",
        qualifiedIdentity("function", tokens, 2, closing),
        null,
      );
    }
    if (
      kind === "DEFAULT" &&
      tokens[2] === "PRIVILEGES" &&
      tokens.slice(3, 9).join("\0") ===
        ["FOR", "ROLE", "CLOUD_AGENTS_MIGRATION_OWNER", "IN", "SCHEMA", "CLOUD_AGENTS"].join(
          "\0",
        ) &&
      new Set([
        ["REVOKE", "ALL", "ON", "TABLES", "FROM", "PUBLIC", ";"].join("\0"),
        ["REVOKE", "ALL", "ON", "SEQUENCES", "FROM", "PUBLIC", ";"].join("\0"),
        ["REVOKE", "EXECUTE", "ON", "FUNCTIONS", "FROM", "PUBLIC", ";"].join("\0"),
      ]).has(tokens.slice(9).join("\0"))
    ) {
      return classification(
        "ALTER",
        "DEFAULT_PRIVILEGES",
        "schema:unquoted:cloud_agents",
        "PUBLIC",
      );
    }
    reject(tokens);
  }
  if (first === "INSERT") {
    const special = EXACT_INSERT_SPECIAL_CASES.get(statement.sha256);
    if (
      !special ||
      migrationId !== special.migrationId ||
      statement.index !== special.statementIndex ||
      tokens[1] !== "INTO"
    ) {
      reject(tokens);
    }
    requireCloudAgentsQualified(tokens, 2);
    const targetIdentity = qualifiedIdentity("table", tokens, 2);
    if (targetIdentity !== special.targetIdentity) reject(tokens);
    return classification("INSERT", "TABLE", targetIdentity, null);
  }
  if (first === "GRANT" || first === "REVOKE") {
    if (
      migrationId === "000004" &&
      tokens.join("\0") ===
        [
          "REVOKE",
          "EXECUTE",
          "ON",
          "ALL",
          "FUNCTIONS",
          "IN",
          "SCHEMA",
          "CLOUD_AGENTS",
          "FROM",
          "PUBLIC",
          ";",
        ].join("\0")
    ) {
      return classification("REVOKE", "ALL_FUNCTIONS", "schema:unquoted:cloud_agents", "PUBLIC");
    }
    const on = findTopLevelToken(tokens, "ON", 1);
    const direction = findTopLevelToken(tokens, first === "GRANT" ? "TO" : "FROM", on + 1);
    const objectKind = tokens[on + 1];
    const grantee = tokens[direction + 1];
    const privileges = tokens.slice(1, on);
    if (
      on < 1 ||
      direction <= on ||
      privileges.length !== 1 ||
      !new Set(["ALL", "USAGE", "SELECT", "EXECUTE"]).has(privileges[0]!) ||
      !objectKind ||
      !new Set(["SCHEMA", "TABLE", "FUNCTION"]).has(objectKind) ||
      !grantee ||
      !ALLOWED_GRANTEES.has(grantee) ||
      tokens[direction + 2] !== ";" ||
      direction + 3 !== tokens.length
    ) {
      reject(tokens);
    }
    if (objectKind === "SCHEMA") {
      if (tokens.slice(on + 2, direction).join("\0") !== "CLOUD_AGENTS") reject(tokens);
    } else {
      requireCloudAgentsQualified(tokens, on + 2);
      if (objectKind === "TABLE" && direction !== on + 5) reject(tokens);
      if (objectKind === "FUNCTION") {
        const closing = matchingCloseParenthesis(tokens, on + 5);
        if (closing !== direction - 1) reject(tokens);
      }
    }
    const targetIdentity =
      objectKind === "SCHEMA"
        ? "schema:unquoted:cloud_agents"
        : qualifiedIdentity(
            objectKind.toLowerCase(),
            tokens,
            on + 2,
            objectKind === "FUNCTION" ? direction - 1 : undefined,
          );
    return classification(first, objectKind, targetIdentity, grantee);
  }
  reject(tokens);
}

function classification(
  command: string,
  objectKind: string,
  targetIdentity: string,
  grantee: string | null,
): SqlStatementClassification {
  return {
    profile: "postgresql-ddl-v1",
    command,
    object_kind: objectKind,
    target_identity: targetIdentity,
    grantee,
    special_case: null,
  };
}

function reject(tokens: ReadonlyArray<string>): never {
  throw new MigrationValidationError(
    "SQL_STATEMENT_PROFILE_REJECTED",
    tokens.slice(0, 12).join(" "),
  );
}

function lexTopLevelTokens(bytes: Uint8Array): string[] {
  const tokens: string[] = [];
  for (let offset = 0; offset < bytes.length;) {
    const byte = bytes[offset]!;
    const next = bytes[offset + 1];
    if (byte === 0x2d && next === 0x2d) {
      offset += 2;
      while (offset < bytes.length && bytes[offset] !== 0x0a) offset += 1;
      continue;
    }
    if (byte === 0x2f && next === 0x2a) {
      offset = skipNestedBlockComment(bytes, offset);
      continue;
    }
    const escapePrefixLength = escapeStringPrefixLength(bytes, offset);
    if (escapePrefixLength > 0) {
      tokens.push("$STRING$");
      offset = skipSingleQuoted(bytes, offset + escapePrefixLength, true);
      continue;
    }
    if (byte === 0x27) {
      tokens.push("$STRING$");
      offset = skipSingleQuoted(bytes, offset, false);
      continue;
    }
    if (byte === 0x22) {
      const result = readQuotedIdentifier(bytes, offset);
      tokens.push(`@quoted:${JSON.stringify(result.value)}`);
      offset = result.end;
      continue;
    }
    if (byte === 0x24) {
      const tag = readDollarTag(bytes, offset);
      if (tag) {
        tokens.push("$BODY$");
        offset = skipDollarBody(bytes, offset, tag);
        continue;
      }
    }
    if (isIdentifierStart(byte)) {
      let end = offset + 1;
      while (end < bytes.length && isIdentifierPart(bytes[end]!)) end += 1;
      tokens.push(ASCII.decode(bytes.slice(offset, end)).toUpperCase());
      offset = end;
      continue;
    }
    if (byte >= 0x30 && byte <= 0x39) {
      let end = offset + 1;
      while (end < bytes.length && bytes[end]! >= 0x30 && bytes[end]! <= 0x39) end += 1;
      tokens.push(ASCII.decode(bytes.slice(offset, end)));
      offset = end;
      continue;
    }
    if (new Set([0x28, 0x29, 0x2c, 0x2e, 0x3b]).has(byte)) tokens.push(String.fromCharCode(byte));
    offset += 1;
  }
  return tokens;
}

function containsSqlToken(bytes: Uint8Array): boolean {
  return lexTopLevelTokens(bytes).length > 0;
}

function readDollarTag(input: Uint8Array, offset: number): Uint8Array | undefined {
  let cursor = offset + 1;
  if (input[cursor] === 0x24) return input.slice(offset, cursor + 1);
  if (input[cursor] === undefined || !isIdentifierStart(input[cursor]!)) return undefined;
  cursor += 1;
  while (cursor < input.length && isDollarTagPart(input[cursor]!)) cursor += 1;
  if (input[cursor] !== 0x24) return undefined;
  return input.slice(offset, cursor + 1);
}

function isIdentifierStart(byte: number): boolean {
  return (byte >= 0x41 && byte <= 0x5a) || (byte >= 0x61 && byte <= 0x7a) || byte === 0x5f;
}

function isIdentifierPart(byte: number): boolean {
  return isIdentifierStart(byte) || (byte >= 0x30 && byte <= 0x39) || byte === 0x24;
}

function isDollarTagPart(byte: number): boolean {
  return isIdentifierStart(byte) || (byte >= 0x30 && byte <= 0x39);
}

function isEscapeStringQuote(input: Uint8Array, quoteOffset: number): boolean {
  if (
    quoteOffset >= 1 &&
    (input[quoteOffset - 1] === 0x45 || input[quoteOffset - 1] === 0x65) &&
    (quoteOffset < 2 || !isIdentifierPart(input[quoteOffset - 2]!))
  ) {
    return true;
  }
  return (
    quoteOffset >= 2 &&
    input[quoteOffset - 1] === 0x26 &&
    (input[quoteOffset - 2] === 0x55 || input[quoteOffset - 2] === 0x75) &&
    (quoteOffset < 3 || !isIdentifierPart(input[quoteOffset - 3]!))
  );
}

function escapeStringPrefixLength(input: Uint8Array, offset: number): number {
  if (
    (input[offset] === 0x45 || input[offset] === 0x65) &&
    input[offset + 1] === 0x27 &&
    (offset === 0 || !isIdentifierPart(input[offset - 1]!))
  ) {
    return 1;
  }
  if (
    (input[offset] === 0x55 || input[offset] === 0x75) &&
    input[offset + 1] === 0x26 &&
    input[offset + 2] === 0x27 &&
    (offset === 0 || !isIdentifierPart(input[offset - 1]!))
  ) {
    return 2;
  }
  return 0;
}

function skipNestedBlockComment(input: Uint8Array, offset: number): number {
  let depth = 1;
  let cursor = offset + 2;
  while (cursor < input.length) {
    if (input[cursor] === 0x2f && input[cursor + 1] === 0x2a) {
      depth += 1;
      cursor += 2;
    } else if (input[cursor] === 0x2a && input[cursor + 1] === 0x2f) {
      depth -= 1;
      cursor += 2;
      if (depth === 0) return cursor;
    } else cursor += 1;
  }
  throw new MigrationValidationError("UNTERMINATED_SQL_LEXEME", "block comment");
}

function skipSingleQuoted(input: Uint8Array, quoteOffset: number, escapes: boolean): number {
  for (let cursor = quoteOffset + 1; cursor < input.length; cursor += 1) {
    if (input[cursor] === 0x27 && input[cursor + 1] === 0x27) cursor += 1;
    else if (escapes && input[cursor] === 0x5c && input[cursor + 1] !== undefined) cursor += 1;
    else if (input[cursor] === 0x27) return cursor + 1;
  }
  throw new MigrationValidationError("UNTERMINATED_SQL_LEXEME", "string");
}

function readQuotedIdentifier(
  input: Uint8Array,
  offset: number,
): { readonly value: string; readonly end: number } {
  const bytes: number[] = [];
  for (let cursor = offset + 1; cursor < input.length; cursor += 1) {
    if (input[cursor] === 0x22 && input[cursor + 1] === 0x22) {
      bytes.push(0x22);
      cursor += 1;
    } else if (input[cursor] === 0x22) {
      return {
        value: new TextDecoder("utf-8", { fatal: true }).decode(Uint8Array.from(bytes)),
        end: cursor + 1,
      };
    } else bytes.push(input[cursor]!);
  }
  throw new MigrationValidationError("UNTERMINATED_SQL_LEXEME", "quoted identifier");
}

function skipDollarBody(input: Uint8Array, offset: number, tag: Uint8Array): number {
  for (let cursor = offset + tag.length; cursor < input.length; cursor += 1) {
    if (matchesAt(input, tag, cursor)) return cursor + tag.length;
  }
  throw new MigrationValidationError("UNTERMINATED_SQL_LEXEME", "dollar body");
}

function requireCloudAgentsQualified(tokens: ReadonlyArray<string>, offset: number): void {
  if (tokens[offset] !== "CLOUD_AGENTS" || tokens[offset + 1] !== "." || !tokens[offset + 2]) {
    reject(tokens);
  }
}

function qualifiedIdentity(
  kind: string,
  tokens: ReadonlyArray<string>,
  offset: number,
  signatureEnd?: number,
): string {
  const base = `${kind}:${canonicalIdentifier(tokens[offset]!)}/${canonicalIdentifier(
    tokens[offset + 2]!,
  )}`;
  if (signatureEnd === undefined) return base;
  const signature = canonicalFunctionSignature(tokens.slice(offset + 4, signatureEnd));
  return `${base}(${signature})`;
}

function canonicalFunctionSignature(tokens: ReadonlyArray<string>): string {
  const groups: string[][] = [[]];
  let depth = 0;
  for (const token of tokens) {
    if (token === "(") depth += 1;
    else if (token === ")") depth -= 1;
    if (token === "," && depth === 0) groups.push([]);
    else groups.at(-1)!.push(token);
  }
  return groups
    .filter((group) => group.length > 0)
    .map((group) => {
      const typeTokens = [...group];
      if (new Set(["IN", "OUT", "INOUT", "VARIADIC"]).has(typeTokens[0]!)) typeTokens.shift();
      if (typeTokens.length >= 2 && isIdentifierToken(typeTokens[0]!)) typeTokens.shift();
      return typeTokens
        .map((token) => (isIdentifierToken(token) ? canonicalIdentifier(token) : token))
        .join("");
    })
    .join(",");
}

function qualifiedDerivedIdentity(
  kind: string,
  tokens: ReadonlyArray<string>,
  qualifiedOffset: number,
  name: string,
): string {
  return `${kind}:${canonicalIdentifier(tokens[qualifiedOffset]!)}/${canonicalIdentifier(name)}`;
}

function canonicalIdentifier(token: string): string {
  if (token.startsWith("@quoted:")) return `quoted:${token.slice("@quoted:".length)}`;
  return `unquoted:${token.toLowerCase()}`;
}

function isIdentifierToken(token: string): boolean {
  return token.startsWith("@quoted:") || /^[A-Z_][A-Z0-9_$]*$/u.test(token);
}

function validateCreatePolicyTail(tokens: ReadonlyArray<string>, offset: number): void {
  let cursor = offset;
  if (tokens[cursor] === "FOR") {
    if (!new Set(["SELECT", "ALL"]).has(tokens[cursor + 1]!)) reject(tokens);
    cursor += 2;
  }
  if (
    tokens[cursor] !== "TO" ||
    !new Set(["CLOUD_AGENTS_RUNTIME", "CLOUD_AGENTS_MIGRATION_OWNER"]).has(tokens[cursor + 1]!) ||
    tokens[cursor + 2] !== "USING"
  ) {
    reject(tokens);
  }
  const usingEnd = matchingCloseParenthesis(tokens, cursor + 3);
  if (usingEnd < 0) reject(tokens);
  const remainder = tokens.slice(usingEnd + 1);
  if (remainder.join("\0") === ";") return;
  if (remainder[0] !== "WITH" || remainder[1] !== "CHECK") reject(tokens);
  const checkEnd = matchingCloseParenthesis(remainder, 2);
  if (checkEnd !== remainder.length - 2 || remainder.at(-1) !== ";") reject(tokens);
}

function matchingCloseParenthesis(tokens: ReadonlyArray<string>, open: number): number {
  if (tokens[open] !== "(") return -1;
  let depth = 0;
  for (let index = open; index < tokens.length; index += 1) {
    if (tokens[index] === "(") depth += 1;
    else if (tokens[index] === ")") {
      depth -= 1;
      if (depth === 0) return index;
    }
  }
  return -1;
}

function hasTopLevelComma(tokens: ReadonlyArray<string>): boolean {
  let depth = 0;
  for (const token of tokens) {
    if (token === "(") depth += 1;
    else if (token === ")") depth -= 1;
    else if (token === "," && depth === 0) return true;
  }
  return false;
}

function findTopLevelToken(tokens: ReadonlyArray<string>, expected: string, start: number): number {
  let depth = 0;
  for (let index = start; index < tokens.length; index += 1) {
    if (tokens[index] === "(") depth += 1;
    else if (tokens[index] === ")") depth -= 1;
    else if (tokens[index] === expected && depth === 0) return index;
  }
  return -1;
}

function matchesAt(input: Uint8Array, expected: Uint8Array, offset: number): boolean {
  return expected.every((byte, index) => input[offset + index] === byte);
}
