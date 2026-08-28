package authn

import (
	"encoding/base64"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func newTrustLineage() *trustLineage {
	return &trustLineage{admitting: true, history: make(map[string][]byte)}
}

func currentSnapshotForTest(lineage *trustLineage) *trustSnapshot {
	lineage.state.Lock()
	defer lineage.state.Unlock()
	if lineage.current == nil {
		return nil
	}
	return lineage.current.snapshot
}

func deterministicModulus(bits int, discriminator byte) []byte {
	modulus := make([]byte, bits/8)
	modulus[0] = 0x80 | discriminator&0x7f
	modulus[len(modulus)-1] = discriminator | 1
	return modulus
}

func deterministicJWK(t *testing.T, kid string, modulus []byte) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": kid,
		"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(modulus), "use": "sig",
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStrictRSAJWK2048And4096AndFaultMatrix(t *testing.T) {
	profile := generatedIdentityVerifierProfile()
	for _, bits := range []int{2048, 4096} {
		raw := deterministicJWK(t, "key-1", deterministicModulus(bits, 1))
		key, ok := parseRSAJWK(raw, true, 10, 20, profile)
		if !ok || key.publicKey.N.BitLen() != bits || key.publicKey.E != 65537 {
			t.Fatalf("valid %d-bit JWK rejected", bits)
		}
	}
	base := map[string]any{
		"alg": "RS256", "e": "AQAB", "key_ops": []string{"verify"}, "kid": "key-1",
		"kty": "RSA", "n": base64.RawURLEncoding.EncodeToString(deterministicModulus(2048, 1)), "use": "sig",
	}
	faults := []func(map[string]any){
		func(v map[string]any) { v["kty"] = "oct" }, func(v map[string]any) { v["alg"] = "rs256" },
		func(v map[string]any) { v["use"] = "enc" }, func(v map[string]any) { v["key_ops"] = []string{"sign"} },
		func(v map[string]any) { v["key_ops"] = []string{"verify", "verify"} }, func(v map[string]any) { v["e"] = "Aw" },
		func(v map[string]any) { v["d"] = "private" }, func(v map[string]any) { delete(v, "use") },
		func(v map[string]any) { v["kid"] = "-bad" }, func(v map[string]any) { v["n"] = "AA" + v["n"].(string) },
		func(v map[string]any) {
			modulus := deterministicModulus(2048, 1)
			modulus[len(modulus)-1] = 2
			v["n"] = base64.RawURLEncoding.EncodeToString(modulus)
		},
		func(v map[string]any) {
			v["n"] = base64.RawURLEncoding.EncodeToString(deterministicModulus(2048, 1)[:255])
		},
		func(v map[string]any) {
			v["n"] = base64.RawURLEncoding.EncodeToString(append([]byte{1}, deterministicModulus(4096, 1)...))
		},
	}
	for index, mutate := range faults {
		candidate := make(map[string]any, len(base))
		for key, value := range base {
			candidate[key] = value
		}
		mutate(candidate)
		raw, _ := json.Marshal(candidate)
		if _, ok := parseRSAJWK(raw, true, 10, 20, profile); ok {
			t.Fatalf("JWK fault %d accepted: %s", index, raw)
		}
	}
	duplicate := []byte(`{"alg":"RS256","e":"AQAB","key_ops":["verify"],"kid":"key-1","\u006bid":"key-2","kty":"RSA","n":"x","use":"sig"}`)
	if _, ok := parseRSAJWK(duplicate, true, 10, 20, profile); ok {
		t.Fatal("duplicate decoded JWK member accepted")
	}
}

func TestTrustGenerationLineageAndDeepImmutability(t *testing.T) {
	modulus := deterministicModulus(2048, 1)
	raw := deterministicJWK(t, "key-1", modulus)
	revoked := []string{"token-old"}
	lineage := newTrustLineage()
	candidate := snapshotCandidate{
		issuer: "https://issuer.example", audience: "https://api.example", generation: 1,
		securityEpoch: 1, notBefore: 100, expiresAt: 200,
		keys: []snapshotKeyCandidate{{jwk: raw, enabled: true, notBefore: 50, notAfter: 250}}, revokedTokenIDs: revoked,
	}
	if err := lineage.replace(candidate); err != nil {
		t.Fatal(err)
	}
	first := currentSnapshotForTest(lineage)
	raw[0] ^= 1
	revoked[0] = "attacker"
	modulus[0] = 0
	if first.keys["key-1"].publicKey.N.BitLen() != 2048 {
		t.Fatal("snapshot retained mutable key input")
	}
	if _, exists := first.revokedTokenIDs["token-old"]; !exists {
		t.Fatal("snapshot retained mutable revocation input")
	}
	secondCandidate := snapshotCandidate{
		issuer: first.issuer, audience: first.audience, generation: 2, previousSnapshotDigest: first.digest,
		securityEpoch: 2, notBefore: 101, expiresAt: 201,
		keys: []snapshotKeyCandidate{{jwk: deterministicJWK(t, "key-1", deterministicModulus(2048, 1)), enabled: true, notBefore: 50, notAfter: 250}},
	}
	if err := lineage.replace(secondCandidate); err != nil {
		t.Fatalf("same kid/material rotation failed: %v", err)
	}
	second := currentSnapshotForTest(lineage)
	third := secondCandidate
	third.generation = 3
	third.previousSnapshotDigest = second.digest
	third.keys = []snapshotKeyCandidate{{jwk: deterministicJWK(t, "key-1", deterministicModulus(2048, 3)), enabled: true, notBefore: 50, notAfter: 250}}
	if err := lineage.replace(third); errorCategory(err) != errorInternalFailure {
		t.Fatal("kid reuse with new material accepted")
	}
	generation, preserved, acquired := lineage.acquireCurrent()
	if !acquired || preserved != second {
		t.Fatal("failed replacement damaged the current generation")
	}
	generation.lease.RUnlock()
	badPrevious := secondCandidate
	badPrevious.generation = 3
	badPrevious.previousSnapshotDigest = first.digest
	if err := lineage.replace(badPrevious); errorCategory(err) != errorInternalFailure {
		t.Fatal("non-immediate previous digest accepted")
	}
}

func TestLifetimeLineageBound32And33(t *testing.T) {
	build := func(count int) snapshotCandidate {
		keys := make([]snapshotKeyCandidate, 0, count)
		for index := range count {
			keys = append(keys, snapshotKeyCandidate{
				jwk:     deterministicJWK(t, "key-"+base64.RawURLEncoding.EncodeToString([]byte{byte(index)}), deterministicModulus(2048, 1)),
				enabled: true, notBefore: 1, notAfter: 100,
			})
		}
		return snapshotCandidate{issuer: "https://issuer.example", audience: "https://api.example", generation: 1, securityEpoch: 1, notBefore: 1, expiresAt: 100, keys: keys}
	}
	if err := newTrustLineage().replace(build(32)); err != nil {
		t.Fatalf("32 lifetime records rejected: %v", err)
	}
	if err := newTrustLineage().replace(build(33)); errorCategory(err) != errorInternalFailure {
		t.Fatal("33 lifetime records accepted")
	}
}

func TestSnapshotCardinalityRejectedBeforeLenDerivedAllocation(t *testing.T) {
	profile := generatedIdentityVerifierProfile()
	base := snapshotCandidate{
		issuer: "https://issuer.example", audience: "https://api.example", generation: 1,
		securityEpoch: 1, notBefore: 1, expiresAt: 2,
	}
	base.keys = make([]snapshotKeyCandidate, int(profile.limits.lifetimeKeyLineageRecords)+1)
	for index := range base.keys {
		base.keys[index].jwk = []byte("sentinel-must-not-be-parsed")
	}
	if _, _, err := buildTrustSnapshot(base, nil, nil); errorCategory(err) != errorInternalFailure {
		t.Fatalf("oversized candidate key slice category=%v", err)
	}
	base.keys = []snapshotKeyCandidate{{jwk: []byte("sentinel-must-not-be-parsed")}}
	oversizedHistory := make(map[string][]byte, int(profile.limits.lifetimeKeyLineageRecords)+1)
	for index := 0; index <= int(profile.limits.lifetimeKeyLineageRecords); index++ {
		oversizedHistory[string(rune('A'+index))] = nil
	}
	if _, _, err := buildTrustSnapshot(base, nil, oversizedHistory); errorCategory(err) != errorInternalFailure {
		t.Fatalf("oversized prior history category=%v", err)
	}
	base.keys = nil
	if _, _, err := buildTrustSnapshot(base, nil, nil); errorCategory(err) != errorInternalFailure {
		t.Fatalf("empty candidate key slice category=%v", err)
	}
}

func TestValidSnapshotCardinalityUsesExactProfileLimit(t *testing.T) {
	profile := generatedIdentityVerifierProfile()
	limit := int(profile.limits.lifetimeKeyLineageRecords)
	counts := []int{0, 1, limit, limit + 1}
	for _, priorCount := range counts {
		for _, keyCount := range counts {
			want := priorCount >= 0 && priorCount <= limit && keyCount >= 1 && keyCount <= limit
			if got := validSnapshotCardinality(profile, priorCount, keyCount); got != want {
				t.Fatalf("validSnapshotCardinality(prior=%d,key=%d)=%t want=%t", priorCount, keyCount, got, want)
			}
		}
	}
	for _, test := range []struct {
		priorCount int
		keyCount   int
	}{
		{priorCount: 1 << 62, keyCount: 1},
		{priorCount: 0, keyCount: 1 << 62},
		{priorCount: -1, keyCount: 1},
	} {
		if validSnapshotCardinality(profile, test.priorCount, test.keyCount) {
			t.Fatalf("out-of-range cardinality accepted: prior=%d key=%d", test.priorCount, test.keyCount)
		}
	}
}

func TestSnapshotCardinalityGuardPrecedesAllocationAndJWKParsing(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "trust.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var build *ast.FuncDecl
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "buildTrustSnapshot" {
			build = function
			break
		}
	}
	if build == nil || build.Body == nil {
		t.Fatal("buildTrustSnapshot body missing")
	}
	var guard *ast.IfStmt
	for _, statement := range build.Body.List {
		candidate, ok := statement.(*ast.IfStmt)
		if ok && exactSnapshotCardinalityGuard(candidate) {
			guard = candidate
			break
		}
	}
	if guard == nil {
		t.Fatal("exact validSnapshotCardinality guard with direct internal-failure return is missing")
	}
	seen := make(map[string]int)
	ast.Inspect(build.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		switch identifier.Name {
		case "make", "append", "copy", "parseRSAJWK":
			seen[identifier.Name]++
			if call.Pos() < guard.End() {
				t.Fatalf("%s at %s precedes the cardinality guard", identifier.Name, fileSet.Position(call.Pos()))
			}
		}
		return true
	})
	if seen["make"] == 0 || seen["parseRSAJWK"] == 0 {
		t.Fatalf("order guard is vacuous: observed calls=%v", seen)
	}
}

func exactSnapshotCardinalityGuard(guard *ast.IfStmt) bool {
	negation, ok := guard.Cond.(*ast.UnaryExpr)
	if !ok || negation.Op != token.NOT || guard.Else != nil {
		return false
	}
	call, ok := negation.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 3 || !exactIdentifier(call.Fun, "validSnapshotCardinality") ||
		!exactIdentifier(call.Args[0], "profile") || !exactLenArgument(call.Args[1], "priorHistory", "") ||
		!exactLenArgument(call.Args[2], "candidate", "keys") {
		return false
	}
	return len(guard.Body.List) == 1 && exactInternalFailureReturn(guard.Body.List[0])
}

func exactLenArgument(expression ast.Expr, owner, field string) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !exactIdentifier(call.Fun, "len") {
		return false
	}
	if field == "" {
		return exactIdentifier(call.Args[0], owner)
	}
	selector, ok := call.Args[0].(*ast.SelectorExpr)
	return ok && exactIdentifier(selector.X, owner) && selector.Sel.Name == field
}

func exactInternalFailureReturn(statement ast.Stmt) bool {
	result, ok := statement.(*ast.ReturnStmt)
	if !ok || len(result.Results) != 3 || !exactIdentifier(result.Results[0], "nil") ||
		!exactIdentifier(result.Results[1], "nil") {
		return false
	}
	call, ok := result.Results[2].(*ast.CallExpr)
	return ok && len(call.Args) == 1 && exactIdentifier(call.Fun, "verifierError") &&
		exactIdentifier(call.Args[0], "errorInternalFailure")
}

func exactIdentifier(expression ast.Expr, name string) bool {
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == name
}

func TestSnapshotCanonicalProjectionGoldenAndBounds(t *testing.T) {
	snapshot, _, err := buildTrustSnapshot(snapshotCandidate{
		issuer: "https://issuer.example", audience: "https://api.example", generation: 1,
		securityEpoch: 7, notBefore: 100, expiresAt: 200,
		keys:          []snapshotKeyCandidate{{jwk: deterministicJWK(t, "key-1", deterministicModulus(2048, 1)), enabled: true, notBefore: 50, notAfter: 250}},
		revokedKeyIDs: []string{"key-1"}, revokedTokenIDs: []string{"token-1"},
	}, nil, map[string][]byte{})
	if err != nil {
		t.Fatal(err)
	}
	wantCanonical := `{"audience":"https://api.example","generation":1,"issuer":"https://issuer.example","keyLineage":[{"e":"AQAB","kid":"key-1","n":"gQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQ"}],"keys":[{"enabled":true,"jwk":{"alg":"RS256","e":"AQAB","key_ops":["verify"],"kid":"key-1","kty":"RSA","n":"gQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAQ","use":"sig"},"notAfter":250,"notBefore":50}],"profileDigest":"sha256:d7da4c6be5048ec8e82e7ace4ef11dc39845843b3718b5e90e4babebd7091459","registryDigest":"sha256:ac468edeca5bc69b15a57a5d2def9d3c372f87a87423cc7922407da7e1aa8dea","revokedKeyIds":["key-1"],"revokedTokenIds":["token-1"],"securityEpoch":7,"validity":{"expiresAt":200,"notBefore":100}}`
	if string(snapshot.canonical) != wantCanonical {
		t.Fatalf("snapshot canonical bytes changed:\n got=%s\nwant=%s", snapshot.canonical, wantCanonical)
	}
	if got, want := snapshot.digest, "sha256:5ccb1de47255b4cbcc8a5cb774f61a0746e81a9e49a78161508b5366b8a5ff5b"; got != want {
		t.Fatalf("snapshot digest changed: got=%s size=%d\ncanonical=%s", got, len(snapshot.canonical), snapshot.canonical)
	}
	canonical := string(snapshot.canonical)
	for _, forbidden := range []string{"snapshotDigest", `"previousSnapshotDigest"`} {
		if strings.Contains(canonical, forbidden) {
			t.Fatalf("generation-one projection contains %q", forbidden)
		}
	}
	if !strings.Contains(canonical, `"revokedKeyIds":["key-1"]`) || !strings.Contains(canonical, `"validity":{"expiresAt":200,"notBefore":100}`) {
		t.Fatal("closed snapshot nesting changed")
	}
	if _, _, err := buildTrustSnapshot(snapshotCandidate{
		issuer: "https://issuer.example", audience: "https://api.example", generation: 1,
		securityEpoch: 1, notBefore: 100, expiresAt: 100,
		keys: []snapshotKeyCandidate{{jwk: deterministicJWK(t, "key-1", deterministicModulus(2048, 1)), enabled: true, notBefore: 1, notAfter: 2}},
	}, nil, map[string][]byte{}); errorCategory(err) != errorInternalFailure {
		t.Fatal("empty snapshot interval accepted")
	}
}
