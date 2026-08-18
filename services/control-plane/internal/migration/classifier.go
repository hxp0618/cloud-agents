package migration

import (
	"fmt"
	"strings"
)

type StatementPlan struct {
	// Legacy structural classifier output. These fields remain for the ADR-0009
	// runner compatibility seam but are insufficient for impl-3 admission.
	Command             string
	ObjectKind          string
	TargetIdentity      string
	Grantee             *string
	MayChangeSchemaACL  bool
	MayChangeDefaultACL bool
	MayChangeOwner      bool

	// Impl-3 exact plan. Only buildExactStatementPlans may set exact and freeze
	// exactCanonical after binding the verified runtime and catalog descriptor.
	MigrationID              string
	StatementIndex           uint32
	SQLArtifactPath          string
	SQLArtifactSHA256        Digest
	SQLArtifactSizeBytes     uint64
	StartOffset              uint64
	EndOffset                uint64
	StatementSHA256          Digest
	Classification           SQLClassificationDescriptor
	ExpectedTransition       ExpectedStatementTransition
	ExpectedTransitionDigest Digest
	exact                    bool
	exactCanonical           string
	sqlBytes                 []byte
}

type StatementClassifier interface {
	Classify(entry MigrationEntry, statement SQLStatement) (StatementPlan, error)
}

type exactStatementPlanSentinel struct {
	Command                  string                      `json:"command"`
	ObjectKind               string                      `json:"object_kind"`
	TargetIdentity           string                      `json:"target_identity"`
	Grantee                  *string                     `json:"grantee"`
	MayChangeSchemaACL       bool                        `json:"may_change_schema_acl"`
	MayChangeDefaultACL      bool                        `json:"may_change_default_acl"`
	MayChangeOwner           bool                        `json:"may_change_owner"`
	MigrationID              string                      `json:"migration_id"`
	StatementIndex           uint32                      `json:"statement_index"`
	SQLArtifactPath          string                      `json:"sql_artifact_path"`
	SQLArtifactSHA256        Digest                      `json:"sql_artifact_sha256"`
	SQLArtifactSizeBytes     uint64                      `json:"sql_artifact_size_bytes"`
	StartOffset              uint64                      `json:"start_offset"`
	EndOffset                uint64                      `json:"end_offset"`
	StatementSHA256          Digest                      `json:"statement_sha256"`
	Classification           SQLClassificationDescriptor `json:"classification"`
	ExpectedTransition       ExpectedStatementTransition `json:"expected_transition"`
	ExpectedTransitionDigest Digest                      `json:"expected_transition_digest"`
}

func (plan StatementPlan) exactSentinel() exactStatementPlanSentinel {
	return exactStatementPlanSentinel{
		Command: plan.Command, ObjectKind: plan.ObjectKind, TargetIdentity: plan.TargetIdentity,
		Grantee: cloneProjectionValue(plan.Grantee), MayChangeSchemaACL: plan.MayChangeSchemaACL,
		MayChangeDefaultACL: plan.MayChangeDefaultACL, MayChangeOwner: plan.MayChangeOwner,
		MigrationID: plan.MigrationID, StatementIndex: plan.StatementIndex,
		SQLArtifactPath: plan.SQLArtifactPath, SQLArtifactSHA256: plan.SQLArtifactSHA256,
		SQLArtifactSizeBytes: plan.SQLArtifactSizeBytes, StartOffset: plan.StartOffset, EndOffset: plan.EndOffset,
		StatementSHA256: plan.StatementSHA256, Classification: cloneProjectionValue(plan.Classification),
		ExpectedTransition: cloneProjectionValue(plan.ExpectedTransition), ExpectedTransitionDigest: plan.ExpectedTransitionDigest,
	}
}

func (plan StatementPlan) validateExact() error {
	if !plan.exact || plan.exactCanonical == "" || !migrationIDPattern.MatchString(plan.MigrationID) || plan.EndOffset <= plan.StartOffset || plan.SQLArtifactPath == "" {
		return fail(CodeInvalidManifest, "statement-plan", "statement plan is not an exact verified plan", nil)
	}
	if err := requireDigest("statement-plan.sql-artifact", plan.SQLArtifactSHA256); err != nil {
		return err
	}
	if err := requireDigest("statement-plan.statement", plan.StatementSHA256); err != nil {
		return err
	}
	if plan.EndOffset > plan.SQLArtifactSizeBytes || plan.EndOffset-plan.StartOffset != uint64(len(plan.sqlBytes)) || DigestBytes(plan.sqlBytes) != plan.StatementSHA256 {
		return fail(CodeInvalidArtifact, "statement-plan", "owned statement bytes differ from exact offsets or digest", nil)
	}
	wantTransitionDigest, err := plan.ExpectedTransition.ComputeDigest()
	if err != nil || wantTransitionDigest != plan.ExpectedTransitionDigest {
		return fail(CodeInvalidManifest, "statement-plan", "expected transition digest differs from its exact transition", err)
	}
	kind := normalizeObjectKind(plan.ObjectKind)
	if plan.Command == "DO" && kind == "SCHEMA" {
		kind = "SCHEMA_BOOTSTRAP"
	}
	if plan.Classification.Profile != "postgresql-ddl-v1" || plan.Command != plan.Classification.Command || kind != plan.Classification.ObjectKind ||
		plan.TargetIdentity != plan.Classification.TargetIdentity || !equalOptionalString(plan.Grantee, plan.Classification.Grantee) {
		return fail(CodeInvalidSQL, "statement-plan", "structural plan differs from its signed classification", nil)
	}
	canonical, err := canonicalContractKey(plan.exactSentinel())
	if err != nil || canonical != plan.exactCanonical {
		return fail(CodeUntrusted, "statement-plan", "statement plan differs from its immutable sentinel", err)
	}
	return nil
}

func freezeExactStatementPlan(structural StatementPlan, entry MigrationEntry, statement SQLStatement, descriptor SQLStatementDescriptor) (StatementPlan, error) {
	if descriptor.Index > uint64(^uint32(0)) {
		return StatementPlan{}, fail(CodeInvalidManifest, entry.ID, "statement index exceeds uint32", nil)
	}
	transitionDigest, err := descriptor.ExpectedTransition.ComputeDigest()
	if err != nil {
		return StatementPlan{}, err
	}
	plan := structural
	plan.MigrationID = entry.ID
	plan.StatementIndex = uint32(descriptor.Index)
	plan.SQLArtifactPath = entry.SQLArtifact.Path
	plan.SQLArtifactSHA256 = entry.SQLArtifact.SHA256
	plan.SQLArtifactSizeBytes = entry.SQLArtifact.SizeBytes
	plan.StartOffset = descriptor.Start
	plan.EndOffset = descriptor.End
	plan.StatementSHA256 = descriptor.SHA256
	plan.Classification = cloneProjectionValue(descriptor.Classification)
	plan.ExpectedTransition = cloneProjectionValue(descriptor.ExpectedTransition)
	plan.ExpectedTransitionDigest = transitionDigest
	plan.exact = true
	plan.sqlBytes = append([]byte(nil), statement.Raw...)
	canonical, err := canonicalContractKey(plan.exactSentinel())
	if err != nil {
		return StatementPlan{}, fail(CodeInvalidManifest, entry.ID, "exact statement plan cannot be canonicalized", err)
	}
	plan.exactCanonical = canonical
	if err := plan.validateExact(); err != nil {
		return StatementPlan{}, err
	}
	return plan, nil
}

func exactStatementPlanEqual(left, right StatementPlan) bool {
	return left.validateExact() == nil && right.validateExact() == nil && left.exactCanonical == right.exactCanonical
}

func (plan StatementPlan) exactSQLBytes() ([]byte, error) {
	if err := plan.validateExact(); err != nil {
		return nil, err
	}
	return append([]byte(nil), plan.sqlBytes...), nil
}

type SpecialStatementIdentity struct {
	MigrationID    string
	StatementIndex int
}

// NarrowDDLClassifier is a structural grammar for the exact postgresql-ddl-v1
// profile. It intentionally mirrors the shared TS classifier; it is not a
// keyword bag and never treats a matching word somewhere in a tail as admission.
type NarrowDDLClassifier struct {
	SpecialDO map[SpecialStatementIdentity]Digest
}

func (classifier NarrowDDLClassifier) Classify(entry MigrationEntry, statement SQLStatement) (StatementPlan, error) {
	tokens := sqlTokenTexts(statement.Tokens)
	if len(tokens) < 2 || tokens[len(tokens)-1] != ";" {
		return StatementPlan{}, rejectSQLProfile(entry.ID, tokens)
	}
	switch tokens[0] {
	case "DO":
		expected, ok := classifier.SpecialDO[SpecialStatementIdentity{MigrationID: entry.ID, StatementIndex: statement.Index}]
		if !ok || expected != statement.SHA256 || len(tokens) != 3 || tokens[1] != "$BODY$" {
			return StatementPlan{}, fail(CodeInvalidSQL, entry.ID, "DO is not the exact signed special-case statement", nil)
		}
		return StatementPlan{Command: "DO", ObjectKind: "SCHEMA", TargetIdentity: "schema:unquoted:cloud_agents", MayChangeSchemaACL: true, MayChangeOwner: true}, nil
	case "CREATE":
		return classifyCreate(entry.ID, statement.Tokens, tokens)
	case "ALTER":
		return classifyAlterStrict(entry.ID, statement.Tokens, tokens)
	case "INSERT":
		return classifyExactCatalogSeed(entry, statement, tokens)
	case "GRANT", "REVOKE":
		return classifyGrantRevoke(entry.ID, statement.Tokens, tokens)
	default:
		return StatementPlan{}, rejectSQLProfile(entry.ID, tokens)
	}
}

func classifyCreate(migrationID string, typed []SQLToken, tokens []string) (StatementPlan, error) {
	kindOffset := 1
	targetOffset := 2
	orReplace := len(tokens) > 4 && tokens[1] == "OR" && tokens[2] == "REPLACE"
	if orReplace {
		if !oneOf(migrationID, "000005", "000006") || tokens[3] != "FUNCTION" {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		kindOffset = 3
		targetOffset = 4
	}
	if len(tokens) <= targetOffset || !oneOf(tokens[kindOffset], "TABLE", "INDEX", "POLICY", "FUNCTION") {
		return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
	}
	kind := tokens[kindOffset]
	switch kind {
	case "TABLE":
		if orReplace || !cloudAgentsQualified(typed, targetOffset) || matchingParen(tokens, targetOffset+3) != len(tokens)-2 {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
	case "FUNCTION":
		if !cloudAgentsQualified(typed, targetOffset) {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		signatureEnd := matchingParen(tokens, targetOffset+3)
		body := lastToken(tokens, "$BODY$")
		if signatureEnd < 0 || body <= signatureEnd || body != len(tokens)-2 || tokens[body-1] != "AS" {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		for _, token := range tokens[signatureEnd+1 : body-1] {
			if oneOf(token, "TABLESPACE", "WITH", "EXTRA", "OWNER", "DROP", "ALTER", "CREATE", "GRANT", "REVOKE", ";") {
				return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
			}
		}
	case "INDEX":
		if orReplace {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		on := topLevelToken(tokens, "ON", 0)
		if on != 3 || !cloudAgentsQualified(typed, on+1) || matchingParen(tokens, on+4) != len(tokens)-2 {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
	case "POLICY":
		if orReplace {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		on := topLevelToken(tokens, "ON", 0)
		if on != 3 || !cloudAgentsQualified(typed, on+1) || !validCreatePolicyTail(tokens, on+4) {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
	}
	plan := StatementPlan{Command: "CREATE", ObjectKind: kind}
	resolved, err := planWithTarget(plan, typed)
	if err != nil {
		return StatementPlan{}, err
	}
	if orReplace {
		expectedReplacement := map[string]string{
			"000005": "function:unquoted:cloud_agents/unquoted:bind_role(unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:text,unquoted:bigint,unquoted:text,unquoted:text,unquoted:timestamptz,unquoted:text,unquoted:text)",
			"000006": "function:unquoted:cloud_agents/unquoted:subject_ref_digest(unquoted:text,unquoted:text,unquoted:text)",
		}[migrationID]
		if resolved.TargetIdentity != expectedReplacement {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
	}
	return resolved, nil
}

func classifyAlterStrict(migrationID string, typed []SQLToken, tokens []string) (StatementPlan, error) {
	if len(tokens) < 3 {
		return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
	}
	kind := tokens[1]
	switch kind {
	case "TABLE":
		if !cloudAgentsQualified(typed, 2) {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		subcommand := tokens[5 : len(tokens)-1]
		exact := stringSliceEqual(subcommand, []string{"OWNER", "TO", "CLOUD_AGENTS_MIGRATION_OWNER"}) ||
			stringSliceEqual(subcommand, []string{"ENABLE", "ROW", "LEVEL", "SECURITY"}) ||
			stringSliceEqual(subcommand, []string{"FORCE", "ROW", "LEVEL", "SECURITY"})
		addConstraint := len(subcommand) >= 3 && subcommand[0] == "ADD" && subcommand[1] == "CONSTRAINT" && !hasTopLevelComma(subcommand[2:])
		dropResourceKindConstraint := migrationID == "000003" &&
			stringSliceEqual(subcommand, []string{"DROP", "CONSTRAINT", "RESOURCE_CHANGES_RESOURCE_KIND"})
		dropAuditFactConstraint := migrationID == "000004" &&
			stringSliceEqual(subcommand, []string{"DROP", "CONSTRAINT", "AUDIT_FACTS_ACTION"}) ||
			migrationID == "000004" &&
				stringSliceEqual(subcommand, []string{"DROP", "CONSTRAINT", "AUDIT_FACTS_RESOURCE_KIND"})
		if !exact && !addConstraint && !dropResourceKindConstraint && !dropAuditFactConstraint {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		plan := StatementPlan{Command: "ALTER", ObjectKind: "TABLE", MayChangeOwner: len(subcommand) > 0 && subcommand[0] == "OWNER"}
		resolved, err := planWithTarget(plan, typed)
		if err != nil {
			return StatementPlan{}, err
		}
		if dropResourceKindConstraint && resolved.TargetIdentity != "table:unquoted:cloud_agents/unquoted:resource_changes" {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		if dropAuditFactConstraint && resolved.TargetIdentity != "table:unquoted:cloud_agents/unquoted:audit_facts" {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		return resolved, nil
	case "FUNCTION":
		if !cloudAgentsQualified(typed, 2) {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		closing := matchingParen(tokens, 5)
		if closing < 0 || !stringSliceEqual(tokens[closing+1:], []string{"OWNER", "TO", "CLOUD_AGENTS_MIGRATION_OWNER", ";"}) {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		return planWithTarget(StatementPlan{Command: "ALTER", ObjectKind: "FUNCTION", MayChangeOwner: true}, typed)
	case "DEFAULT":
		prefix := []string{"ALTER", "DEFAULT", "PRIVILEGES", "FOR", "ROLE", "CLOUD_AGENTS_MIGRATION_OWNER", "IN", "SCHEMA", "CLOUD_AGENTS"}
		if len(tokens) < len(prefix) || !stringSliceEqual(tokens[:len(prefix)], prefix) {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		tail := tokens[len(prefix):]
		allowed := stringSliceEqual(tail, []string{"REVOKE", "ALL", "ON", "TABLES", "FROM", "PUBLIC", ";"}) ||
			stringSliceEqual(tail, []string{"REVOKE", "ALL", "ON", "SEQUENCES", "FROM", "PUBLIC", ";"}) ||
			stringSliceEqual(tail, []string{"REVOKE", "EXECUTE", "ON", "FUNCTIONS", "FROM", "PUBLIC", ";"})
		if !allowed {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		grantee := "PUBLIC"
		return StatementPlan{Command: "ALTER", ObjectKind: "DEFAULT PRIVILEGES", TargetIdentity: "schema:unquoted:cloud_agents", Grantee: &grantee, MayChangeDefaultACL: true}, nil
	default:
		return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
	}
}

func classifyExactCatalogSeed(entry MigrationEntry, statement SQLStatement, tokens []string) (StatementPlan, error) {
	if len(tokens) < 6 || tokens[1] != "INTO" || !cloudAgentsQualified(statement.Tokens, 2) {
		return StatementPlan{}, rejectSQLProfile(entry.ID, tokens)
	}
	wantTarget := ""
	switch {
	case entry.ID == "000003" && statement.Index == 44 && statement.SHA256 == Digest("sha256:004150417e326e671f4a8aa198ab9c8f955dedfa21966f3525b9ddf451d393be"):
		wantTarget = "table:unquoted:cloud_agents/unquoted:builtin_roles"
	case entry.ID == "000003" && statement.Index == 45 && statement.SHA256 == Digest("sha256:0e9974a61b7e24895ab1c824c89b35c74d52bf6b49b51b0d675134eb7796b8a8"):
		wantTarget = "table:unquoted:cloud_agents/unquoted:builtin_role_permissions"
	default:
		return StatementPlan{}, rejectSQLProfile(entry.ID, tokens)
	}
	plan, err := planWithTarget(StatementPlan{Command: "INSERT", ObjectKind: "TABLE"}, statement.Tokens)
	if err != nil || plan.TargetIdentity != wantTarget {
		return StatementPlan{}, rejectSQLProfile(entry.ID, tokens)
	}
	return plan, nil
}

func classifyGrantRevoke(migrationID string, typed []SQLToken, tokens []string) (StatementPlan, error) {
	if migrationID == "000004" && stringSliceEqual(tokens, []string{
		"REVOKE", "EXECUTE", "ON", "ALL", "FUNCTIONS", "IN", "SCHEMA", "CLOUD_AGENTS", "FROM", "PUBLIC", ";",
	}) {
		grantee := "PUBLIC"
		return StatementPlan{
			Command:        "REVOKE",
			ObjectKind:     "ALL_FUNCTIONS",
			TargetIdentity: "schema:unquoted:cloud_agents",
			Grantee:        &grantee,
		}, nil
	}
	on := topLevelToken(tokens, "ON", 1)
	directionWord := "TO"
	if tokens[0] == "REVOKE" {
		directionWord = "FROM"
	}
	direction := topLevelToken(tokens, directionWord, on+1)
	if on < 1 || direction <= on || direction+3 != len(tokens) || tokens[direction+2] != ";" {
		return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
	}
	privileges := tokens[1:on]
	objectKind := tokens[on+1]
	grantee := tokens[direction+1]
	if len(privileges) != 1 || !oneOf(privileges[0], "ALL", "USAGE", "SELECT", "EXECUTE") ||
		!oneOf(objectKind, "SCHEMA", "TABLE", "FUNCTION") ||
		!oneOf(grantee, "PUBLIC", "CLOUD_AGENTS_RUNTIME", "CLOUD_AGENTS_BOOTSTRAP_ADMIN") {
		return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
	}
	if objectKind == "SCHEMA" {
		if direction != on+3 || tokens[on+2] != "CLOUD_AGENTS" {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
	} else {
		if !cloudAgentsQualified(typed, on+2) {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		if objectKind == "TABLE" && direction != on+5 {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
		if objectKind == "FUNCTION" && matchingParen(tokens, on+5) != direction-1 {
			return StatementPlan{}, rejectSQLProfile(migrationID, tokens)
		}
	}
	plan := StatementPlan{Command: tokens[0], ObjectKind: objectKind, Grantee: &grantee, MayChangeSchemaACL: objectKind == "SCHEMA"}
	return planWithTarget(plan, typed)
}

func validCreatePolicyTail(tokens []string, offset int) bool {
	cursor := offset
	if cursor < len(tokens) && tokens[cursor] == "FOR" {
		if cursor+1 >= len(tokens) || !oneOf(tokens[cursor+1], "SELECT", "ALL") {
			return false
		}
		cursor += 2
	}
	if cursor+3 >= len(tokens) || tokens[cursor] != "TO" || !oneOf(tokens[cursor+1], "CLOUD_AGENTS_RUNTIME", "CLOUD_AGENTS_MIGRATION_OWNER") || tokens[cursor+2] != "USING" {
		return false
	}
	usingEnd := matchingParen(tokens, cursor+3)
	if usingEnd < 0 {
		return false
	}
	remainder := tokens[usingEnd+1:]
	if stringSliceEqual(remainder, []string{";"}) {
		return true
	}
	if len(remainder) < 4 || remainder[0] != "WITH" || remainder[1] != "CHECK" {
		return false
	}
	checkEnd := matchingParen(remainder, 2)
	return checkEnd == len(remainder)-2 && remainder[len(remainder)-1] == ";"
}

func planWithTarget(plan StatementPlan, tokens []SQLToken) (StatementPlan, error) {
	target, err := deriveTargetIdentity(plan, tokens)
	if err != nil {
		return StatementPlan{}, err
	}
	plan.TargetIdentity = target
	return plan, nil
}

func deriveTargetIdentity(plan StatementPlan, typed []SQLToken) (string, error) {
	tokens := sqlTokenTexts(typed)
	if plan.Command == "DO" || plan.ObjectKind == "DEFAULT PRIVILEGES" || plan.ObjectKind == "SCHEMA" {
		return "schema:unquoted:cloud_agents", nil
	}
	start, derivedName := -1, -1
	switch plan.Command {
	case "CREATE":
		if plan.ObjectKind == "FUNCTION" && len(typed) > 4 && typed[1].Text == "OR" && typed[2].Text == "REPLACE" && typed[3].Text == "FUNCTION" {
			start = 4
		} else if plan.ObjectKind == "TABLE" || plan.ObjectKind == "FUNCTION" {
			start = 2
		} else {
			derivedName = 2
			start = topLevelToken(tokens, "ON", 0) + 1
		}
	case "ALTER":
		start = 2
	case "INSERT":
		start = 2
	case "GRANT", "REVOKE":
		start = topLevelToken(tokens, "ON", 0) + 2
	}
	if start < 0 || start+2 >= len(typed) || typed[start+1].Text != "." || !isIdentifierSQLToken(typed[start]) || !isIdentifierSQLToken(typed[start+2]) {
		return "", fail(CodeInvalidSQL, "target-identity", "statement target is not an exact qualified identity", nil)
	}
	kind := strings.ToLower(plan.ObjectKind)
	if derivedName >= 0 {
		if derivedName >= len(typed) || !isIdentifierSQLToken(typed[derivedName]) {
			return "", fail(CodeInvalidSQL, "target-identity", "derived target name is invalid", nil)
		}
		return kind + ":" + canonicalSQLIdentifier(typed[start]) + "/" + canonicalSQLIdentifier(typed[derivedName]), nil
	}
	base := kind + ":" + canonicalSQLIdentifier(typed[start]) + "/" + canonicalSQLIdentifier(typed[start+2])
	if plan.ObjectKind != "FUNCTION" {
		return base, nil
	}
	open := start + 3
	close := matchingParen(tokens, open)
	if close < 0 {
		return "", fail(CodeInvalidSQL, "target-identity", "function signature is unterminated", nil)
	}
	return base + "(" + canonicalFunctionSignature(typed[open+1:close]) + ")", nil
}

func canonicalFunctionSignature(tokens []SQLToken) string {
	groups := make([][]SQLToken, 1)
	depth := 0
	for _, token := range tokens {
		if token.Text == "(" {
			depth++
		} else if token.Text == ")" {
			depth--
		}
		if token.Text == "," && depth == 0 {
			groups = append(groups, nil)
			continue
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], token)
	}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		if isWordOneOf(group[0], "IN", "OUT", "INOUT", "VARIADIC") {
			group = group[1:]
		}
		if len(group) >= 2 && isIdentifierSQLToken(group[0]) {
			group = group[1:]
		}
		var part strings.Builder
		for _, token := range group {
			if isIdentifierSQLToken(token) {
				part.WriteString(canonicalSQLIdentifier(token))
			} else {
				part.WriteString(token.Text)
			}
		}
		parts = append(parts, part.String())
	}
	return strings.Join(parts, ",")
}

func canonicalSQLIdentifier(token SQLToken) string {
	if token.Kind == SQLWord {
		return "unquoted:" + strings.ToLower(token.Text)
	}
	return "quoted:" + strings.TrimPrefix(token.Text, "@quoted:")
}

func sqlTokenTexts(tokens []SQLToken) []string {
	result := make([]string, len(tokens))
	for index := range tokens {
		result[index] = tokens[index].Text
	}
	return result
}

func cloudAgentsQualified(tokens []SQLToken, offset int) bool {
	return offset >= 0 && offset+2 < len(tokens) && tokens[offset].Kind == SQLWord && tokens[offset].Text == "CLOUD_AGENTS" && tokens[offset+1].Text == "." && isIdentifierSQLToken(tokens[offset+2])
}

func isIdentifierSQLToken(token SQLToken) bool {
	return token.Kind == SQLWord || token.Kind == SQLQuotedIdentifier
}

func isWordOneOf(token SQLToken, expected ...string) bool {
	return token.Kind == SQLWord && oneOf(token.Text, expected...)
}

func matchingParen(tokens []string, open int) int {
	if open < 0 || open >= len(tokens) || tokens[open] != "(" {
		return -1
	}
	depth := 0
	for index := open; index < len(tokens); index++ {
		if tokens[index] == "(" {
			depth++
		} else if tokens[index] == ")" {
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func topLevelToken(tokens []string, expected string, start int) int {
	depth := 0
	for index := max(start, 0); index < len(tokens); index++ {
		if tokens[index] == "(" {
			depth++
		} else if tokens[index] == ")" {
			depth--
		} else if depth == 0 && tokens[index] == expected {
			return index
		}
	}
	return -1
}

func lastToken(tokens []string, expected string) int {
	for index := len(tokens) - 1; index >= 0; index-- {
		if tokens[index] == expected {
			return index
		}
	}
	return -1
}

func hasTopLevelComma(tokens []string) bool {
	depth := 0
	for _, token := range tokens {
		if token == "(" {
			depth++
		} else if token == ")" {
			depth--
		} else if token == "," && depth == 0 {
			return true
		}
	}
	return false
}

func stringSliceEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func rejectSQLProfile(migrationID string, tokens []string) error {
	if len(tokens) > 12 {
		tokens = tokens[:12]
	}
	return fail(CodeInvalidSQL, migrationID, fmt.Sprintf("statement is outside postgresql-ddl-v1: %s", strings.Join(tokens, " ")), nil)
}

func oneOf(value string, expected ...string) bool {
	for _, current := range expected {
		if value == current {
			return true
		}
	}
	return false
}
