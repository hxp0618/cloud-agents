package migration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	postgresProjectionAuthoritySubject Digest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	postgresProjectionSchemaSubject    Digest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	postgresProjectionDatabase                = "cag_projection"
	postgresProjectionLogin                   = "cag_migration"
	postgresProjectionDatabaseOwner           = "cag_db_owner"
)

type postgresProjectionFixture struct {
	AuthorityExpected AuthorityExpectedProjections
	SchemaAbsent      CatalogStateProjection
	SchemaPresent     CatalogStateProjection
	CatalogRaw        []byte
}

type postgresProjectionEnvironment struct {
	AdminURL       string
	MigrationURL   string
	Instance       string
	Major          uint16
	ServerVersion  uint32
	ImageID        string
	ContainerArch  string
	ExpectedLocale string
}

func TestPGProjectionPostgresCheckedInExpected(t *testing.T) {
	for _, major := range []uint16{15, 16, 17} {
		t.Run(strconv.FormatUint(uint64(major), 10), func(t *testing.T) {
			fixture := loadPostgresProjectionFixture(t, major)
			condition := CatalogPrecondition{AcceptedStates: []CatalogStateProjection{fixture.SchemaAbsent, fixture.SchemaPresent}}
			verified, err := bindVerifiedSchemaBundleScope(
				postgresProjectionSchemaSubject, fixture.SchemaAbsent.Absent.Scope, condition,
				[]string{MigrationOwnerRole}, []string{MigrationOwnerRole, "postgres"},
				time.Now().Add(time.Hour), 1,
			)
			if err != nil {
				t.Fatalf("bind checked-in PG%d predecessor: %v", major, err)
			}
			if err := verified.validatePrecondition(verified.BoundPrecondition()); err != nil {
				t.Fatalf("validate checked-in PG%d predecessor wrapper: %v", major, err)
			}
			subject, err := bindVerifiedExecutableCatalogSubject(fixture.CatalogRaw, DigestBytes(fixture.CatalogRaw), time.Now().Add(time.Hour), 1, time.Now())
			if err != nil {
				t.Fatalf("bind checked-in PG%d representative catalog subject: %v", major, err)
			}
			combined, err := bindVerifiedCatalogContractWithOwners(
				subject.artifactDigest, subject.verifiedCatalog.Scope(), subject.verifiedCatalog.ExpectedProjection(),
				[]string{MigrationOwnerRole}, []string{MigrationOwnerRole, "postgres"},
				subject.verifiedCatalog.verifiedDecisionExpiresAt, subject.verifiedCatalog.verifiedDecisionSecurityEpoch,
			)
			if err != nil || combined.validate() != nil {
				t.Fatalf("combine checked-in PG%d representative catalog authority: contract=%v validate=%v", major, err, combined.validate())
			}
		})
	}
}

func TestPGProjectionPostgresMatrix(t *testing.T) {
	environment := requirePostgresProjectionEnvironment(t)
	fixture := loadPostgresProjectionFixture(t, environment.Major)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, environment.AdminURL)
	if err != nil {
		t.Fatalf("connect local PostgreSQL matrix admin: %v", err)
	}
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = admin.Close(closeCtx)
	}()
	restorePostgresProjectionBaseline(t, ctx, admin, environment.Major)
	defer restorePostgresProjectionBaseline(t, context.Background(), admin, environment.Major)

	connectedPool := newPostgresProjectionPool(t, ctx, environment.MigrationURL, false)
	defer connectedPool.Close()
	authorityContract, err := bindVerifiedAuthorityContract(
		postgresProjectionAuthoritySubject,
		fixture.AuthorityExpected,
		time.Now().Add(time.Hour),
		1,
	)
	if err != nil {
		t.Fatalf("bind checked-in authority expected projections to verified wrapper: %v", err)
	}
	schemaScope := fixture.SchemaAbsent.Absent.Scope
	condition := CatalogPrecondition{AcceptedStates: []CatalogStateProjection{fixture.SchemaAbsent, fixture.SchemaPresent}}
	verifiedSchema, err := bindVerifiedSchemaBundleScope(
		postgresProjectionSchemaSubject, schemaScope, condition,
		[]string{MigrationOwnerRole}, []string{MigrationOwnerRole, "postgres"},
		time.Now().Add(time.Hour), 1,
	)
	if err != nil {
		t.Fatalf("bind checked-in predecessor accepted_states to verified wrapper: %v", err)
	}
	condition = verifiedSchema.BoundPrecondition()
	catalogSubject, err := bindVerifiedExecutableCatalogSubject(fixture.CatalogRaw, DigestBytes(fixture.CatalogRaw), time.Now().Add(time.Hour), 1, time.Now())
	if err != nil {
		t.Fatalf("bind checked-in representative catalog subject: %v", err)
	}
	verifiedCatalog, err := bindVerifiedCatalogContractWithOwners(
		catalogSubject.artifactDigest, catalogSubject.verifiedCatalog.Scope(), catalogSubject.verifiedCatalog.ExpectedProjection(),
		[]string{MigrationOwnerRole}, []string{MigrationOwnerRole, "postgres"},
		catalogSubject.verifiedCatalog.verifiedDecisionExpiresAt, catalogSubject.verifiedCatalog.verifiedDecisionSecurityEpoch,
	)
	if err != nil {
		t.Fatalf("combine representative catalog with signed owner closure: %v", err)
	}

	connectedDigest := projectPostgresAuthorityIdle(t, ctx, connectedPool, authorityContract, AuthorityPhaseConnectedSession, environment)
	migrationRoleDigest := projectPostgresAuthorityIdle(t, ctx, connectedPool, authorityContract, AuthorityPhaseMigrationRole, environment)
	runnerConnectedDigest, runnerMigrationRoleDigest := projectPostgresRunnerSessionAuthority(t, ctx, environment, authorityContract)
	if runnerConnectedDigest != connectedDigest || runnerMigrationRoleDigest != migrationRoleDigest {
		t.Fatalf("same dedicated runner connection projection drifted: connected=%s/%s migration_role=%s/%s", runnerConnectedDigest, connectedDigest, runnerMigrationRoleDigest, migrationRoleDigest)
	}
	runPostgresPublicRunnerAuthorityPreflight(t, ctx, admin, environment, fixture.AuthorityExpected)
	absentDigest := projectPostgresPreconditionIdle(t, ctx, connectedPool, verifiedSchema, condition, fixture.SchemaAbsent, environment)

	migrationTxDigest, ownedWriteDigest := projectPostgresBorrowedTransaction(
		t, ctx, environment, authorityContract, verifiedSchema, condition, fixture.SchemaPresent,
	)
	assertPostgresSchemaAbsent(t, ctx, admin)

	createPostgresProjectionSchema(t, ctx, admin)
	presentDigest := projectPostgresPreconditionIdle(t, ctx, connectedPool, verifiedSchema, condition, fixture.SchemaPresent, environment)
	if ownedWriteDigest != presentDigest {
		t.Fatalf("borrowed transaction and committed idle projection differ: borrowed=%s idle=%s", ownedWriteDigest, presentDigest)
	}

	t.Run("drift", func(t *testing.T) {
		testPostgresAuthorityDrift(t, ctx, admin, connectedPool, authorityContract, environment)
		testPostgresCatalogDrift(t, ctx, admin, connectedPool, verifiedSchema, condition, environment)
	})
	execPostgresStatements(t, ctx, admin, "DROP SCHEMA cloud_agents CASCADE")
	createPostgresProjectionSchema(t, ctx, admin)
	createPostgresRepresentativeCatalog(t, ctx, admin)
	catalogIdleDigest := projectPostgresCatalogIdle(t, ctx, connectedPool, verifiedCatalog, environment)
	catalogMigrationDigest := projectPostgresCatalogBorrowed(t, ctx, environment, verifiedCatalog)
	if catalogIdleDigest != catalogMigrationDigest {
		t.Fatalf("idle and borrowed full catalog projections differ: idle=%s migration=%s", catalogIdleDigest, catalogMigrationDigest)
	}
	t.Run("catalog-full-drift", func(t *testing.T) {
		testPostgresFullCatalogDrift(t, ctx, admin, connectedPool, verifiedCatalog, environment)
	})
	t.Run("snapshot-cleanup", func(t *testing.T) {
		testPostgresCanceledSnapshotCleanup(t, ctx, connectedPool, environment)
		testPostgresTerminatedBackendHijack(t, ctx, admin, environment)
	})

	summaryInput := strings.Join([]string{
		string(connectedDigest), string(migrationRoleDigest), string(migrationTxDigest),
		string(absentDigest), string(presentDigest), string(catalogIdleDigest), string(catalogMigrationDigest),
	}, "|")
	summary := sha256.Sum256([]byte(summaryInput))
	t.Logf("POSTGRES_PROJECTION_SAME_BITS instance=%s major=%d digest=sha256:%x authority_connected=%s authority_migration_role=%s authority_migration_tx=%s schema_absent=%s schema_present=%s catalog_idle=%s catalog_migration=%s catalog_subject=%s",
		environment.Instance, environment.Major, summary, connectedDigest, migrationRoleDigest, migrationTxDigest, absentDigest, presentDigest, catalogIdleDigest, catalogMigrationDigest, verifiedCatalog.SubjectDigest())
}

func requirePostgresProjectionEnvironment(t *testing.T) postgresProjectionEnvironment {
	t.Helper()
	adminURL := os.Getenv("CLOUD_AGENTS_PROJECTION_ADMIN_URL")
	migrationURL := os.Getenv("CLOUD_AGENTS_PROJECTION_MIGRATION_URL")
	if adminURL == "" || migrationURL == "" {
		if os.Getenv("CLOUD_AGENTS_REQUIRE_POSTGRES_PROJECTION_TEST") == "1" {
			t.Fatal("CLOUD_AGENTS_PROJECTION_ADMIN_URL and CLOUD_AGENTS_PROJECTION_MIGRATION_URL are required")
		}
		t.Skip("local PostgreSQL projection matrix is not configured")
	}
	major64, err := strconv.ParseUint(os.Getenv("CLOUD_AGENTS_EXPECTED_POSTGRES_MAJOR"), 10, 16)
	if err != nil || major64 < 15 || major64 > 17 {
		t.Fatalf("invalid CLOUD_AGENTS_EXPECTED_POSTGRES_MAJOR: %q", os.Getenv("CLOUD_AGENTS_EXPECTED_POSTGRES_MAJOR"))
	}
	version64, err := strconv.ParseUint(os.Getenv("CLOUD_AGENTS_EXPECTED_POSTGRES_VERSION_NUM"), 10, 32)
	if err != nil || version64/10_000 != major64 {
		t.Fatalf("invalid CLOUD_AGENTS_EXPECTED_POSTGRES_VERSION_NUM: %q", os.Getenv("CLOUD_AGENTS_EXPECTED_POSTGRES_VERSION_NUM"))
	}
	environment := postgresProjectionEnvironment{
		AdminURL: adminURL, MigrationURL: migrationURL, Instance: os.Getenv("CLOUD_AGENTS_PROJECTION_INSTANCE"),
		Major: uint16(major64), ServerVersion: uint32(version64), ImageID: os.Getenv("CLOUD_AGENTS_PROJECTION_IMAGE_ID"),
		ContainerArch: os.Getenv("CLOUD_AGENTS_PROJECTION_CONTAINER_ARCH"), ExpectedLocale: os.Getenv("CLOUD_AGENTS_PROJECTION_PROFILE"),
	}
	if environment.Instance != "A" && environment.Instance != "B" {
		t.Fatalf("invalid projection matrix instance: %q", environment.Instance)
	}
	if !strings.HasPrefix(environment.ImageID, "sha256:") || environment.ContainerArch == "" || environment.ExpectedLocale != "UTF8/C/C" {
		t.Fatalf("incomplete immutable image/profile evidence: image=%q arch=%q profile=%q", environment.ImageID, environment.ContainerArch, environment.ExpectedLocale)
	}
	return environment
}

func loadPostgresProjectionFixture(t *testing.T, major uint16) postgresProjectionFixture {
	t.Helper()
	catalogRaw, err := os.ReadFile(filepath.Join("testdata", "postgres_projection", "catalog-representative.json"))
	if err != nil {
		t.Fatalf("read checked-in representative catalog: %v", err)
	}
	catalog, err := DecodeCatalogContract(catalogRaw)
	if err != nil {
		t.Fatalf("strict-decode checked-in representative catalog: %v", err)
	}
	scope := postgresRepresentativeCatalogScope()
	if catalog.ExpectedProjection.SchemaHead != *scope.SchemaHead || !equalObjectIdentityClosures(catalog.DeclaredObjectIdentities, scope.DeclaredObjects) || !equalObjectIdentityClosures(catalog.ExpectedProjection.Body.DeclaredObjects, scope.DeclaredObjects) {
		t.Fatal("checked-in representative catalog differs from its DDL scope")
	}
	return postgresProjectionFixture{
		AuthorityExpected: loadPostgresAuthorityExpected(t, major),
		SchemaAbsent:      loadPostgresProjectionJSON[CatalogStateProjection](t, "schema-absent.json"),
		SchemaPresent:     loadPostgresProjectionJSON[CatalogStateProjection](t, "schema-present.json"),
		CatalogRaw:        append([]byte(nil), catalogRaw...),
	}
}

func loadPostgresAuthorityExpected(t *testing.T, major uint16) AuthorityExpectedProjections {
	t.Helper()
	name := fmt.Sprintf("authority-pg%d.json", major)
	if major >= 16 {
		name = "authority-pg16-pg17.json"
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "postgres_projection", name))
	if err != nil {
		t.Fatalf("read checked-in authority fixture %s: %v", name, err)
	}
	var expected AuthorityExpectedProjections
	if _, err := DecodeStrict(raw, &expected); err != nil {
		t.Fatalf("strict-decode checked-in authority fixture %s: %v", name, err)
	}
	for phase, projection := range map[AuthorityPhase]AuthorityProjection{
		AuthorityPhaseConnectedSession:     expected.ConnectedSession,
		AuthorityPhaseMigrationRole:        expected.MigrationRole,
		AuthorityPhaseMigrationTransaction: expected.MigrationTransaction,
	} {
		validationErr := projection.Validate()
		if projection.Phase != phase || validationErr != nil {
			t.Fatalf("checked-in authority fixture %s has invalid %s projection: phase=%s err=%v", name, phase, projection.Phase, validationErr)
		}
	}
	return expected
}

func loadPostgresProjectionJSON[T interface{ Validate() error }](t *testing.T, name string) T {
	t.Helper()
	var projection T
	raw, err := os.ReadFile(filepath.Join("testdata", "postgres_projection", name))
	if err != nil {
		t.Fatalf("read checked-in projection fixture %s: %v", name, err)
	}
	if _, err := DecodeStrict(raw, &projection); err != nil {
		t.Fatalf("strict-decode checked-in projection fixture %s: %v", name, err)
	}
	if err := projection.Validate(); err != nil {
		t.Fatalf("validate checked-in projection fixture %s: %v", name, err)
	}
	return projection
}

func newPostgresProjectionPool(t *testing.T, ctx context.Context, dsn string, setMigrationRole bool) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse local projection pool configuration: %v", err)
	}
	config.MaxConns = 1
	config.MinConns = 0
	if setMigrationRole {
		t.Fatal("owned idle snapshots require a clean pool; SET ROLE must be owned by the migration transaction path")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open local projection pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping local projection pool: %v", err)
	}
	return pool
}

func projectPostgresAuthorityIdle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, contract VerifiedAuthorityContract, phase AuthorityPhase, environment postgresProjectionEnvironment) Digest {
	t.Helper()
	snapshot, err := BeginIdleReadSnapshot(ctx, pool, phase)
	if err != nil {
		t.Fatalf("begin %s authority snapshot: %v", phase, err)
	}
	defer func() {
		if err := snapshot.RollbackAndRelease(context.Background()); err != nil {
			t.Fatalf("rollback %s authority snapshot: %v", phase, err)
		}
	}()
	assertPostgresSnapshotMetadata(t, snapshot.Metadata(), environment, phase, IdleReadSnapshot)
	projector, err := NewPGProjector(ctx, snapshot)
	if err != nil {
		t.Fatalf("probe PG%d capability for %s: %v", environment.Major, phase, err)
	}
	assertPostgresCapabilities(t, projector, environment)
	result, err := projector.ProjectAuthority(ctx, snapshot, contract, phase)
	if err != nil {
		t.Fatalf("project %s authority after query_count=%d: %v", phase, snapshot.projectionStats().QueryCount, err)
	}
	if result.Metadata.QueryCount != 5 || result.Metadata.Scope != nil || result.Metadata.Snapshot.TxStatus != "T" {
		t.Fatalf("unexpected %s authority metadata: %+v", phase, result.Metadata)
	}
	want, err := digestProjectionWrapper(AuthorityProjectionDigestDomain, contract.expectedProjectionForTest(phase))
	if err != nil || result.Digest != want {
		t.Fatalf("%s authority digest differs from checked-in fixture: got=%s want=%s err=%v", phase, result.Digest, want, err)
	}
	return result.Digest
}

func projectPostgresRunnerSessionAuthority(t *testing.T, ctx context.Context, environment postgresProjectionEnvironment, contract VerifiedAuthorityContract) (Digest, Digest) {
	t.Helper()
	session, err := (PGXConnector{}).Connect(ctx, environment.MigrationURL)
	if err != nil {
		t.Fatalf("connect dedicated runner projection session: %v", err)
	}
	defer func() { _ = session.Close(context.Background()) }()
	project := func(phase AuthorityPhase) Digest {
		snapshot, err := BeginRunnerSessionProjectionSnapshot(ctx, session, phase)
		if err != nil {
			t.Fatalf("begin dedicated runner %s projection: %v", phase, err)
		}
		returned := false
		defer func() {
			if !returned {
				_ = snapshot.RollbackAndReturnToRunner(context.Background())
			}
		}()
		projector, err := NewPGProjector(ctx, snapshot)
		if err != nil {
			t.Fatalf("construct dedicated runner %s projector: %v", phase, err)
		}
		result, err := projector.ProjectAuthority(ctx, snapshot, contract, phase)
		if err != nil {
			t.Fatalf("project dedicated runner %s authority: %v", phase, err)
		}
		if err := snapshot.RollbackAndReturnToRunner(ctx); err != nil {
			t.Fatalf("return dedicated runner %s session: %v", phase, err)
		}
		returned = true
		assertPostgresSnapshotMetadata(t, result.Metadata.Snapshot, environment, phase, IdleReadSnapshot)
		return result.Digest
	}
	connected := project(AuthorityPhaseConnectedSession)
	policy := ExecutionPolicy{StatementTimeoutMS: 5000, LockTimeoutMS: 1000, IdleInTransactionSessionTimeoutMS: 60000}
	if err := session.SetRoleAndSettings(ctx, policy); err != nil {
		t.Fatalf("configure dedicated runner migration role: %v", err)
	}
	const advisoryKey int64 = 0x102030405060709
	if err := session.AcquireAdvisoryLock(ctx, advisoryKey); err != nil {
		t.Fatalf("acquire dedicated runner projection lock: %v", err)
	}
	migrationRole := project(AuthorityPhaseMigrationRole)
	if err := session.UnlockAndReset(ctx, advisoryKey); err != nil {
		t.Fatalf("release dedicated runner projection lock: %v", err)
	}
	return connected, migrationRole
}

func runPostgresPublicRunnerAuthorityPreflight(t *testing.T, ctx context.Context, admin *pgx.Conn, environment postgresProjectionEnvironment, expected AuthorityExpectedProjections) {
	t.Helper()
	raw, decision := buildExactAdmissionRuntimeWithAuthority(t, &expected)
	verifier := &sequenceTrustVerifier{fallback: decision, recoveryArtifact: runnerDecisionRecoveryArtifact(t, decision)}
	sink := &runnerEvidenceSinkFake{}
	observer := &recordingStateObserver{}
	before := liveVerifiedEvidenceRunBindings()
	runner := Runner{Trust: verifier, Evidence: sink, Connector: PGXConnector{}, Observer: observer}
	_, err := runner.Run(ctx, RunRequest{Artifact: &memoryArtifactSource{data: raw}, TargetDSN: environment.MigrationURL})
	var migrationErr *Error
	if !errors.As(err, &migrationErr) || migrationErr.Code != CodeInvalidSQL || migrationErr.Op != "runner-statement-execute" || migrationErr.Err != nil {
		t.Fatalf("public runner authority preflight boundary: %#v", migrationErr)
	}
	if sink.session == nil || sink.session.bindCalls != 1 || sink.session.journal.appendCalls != 1 || sink.session.closeCalls != 1 || sink.session.journal.closeCalls != 1 || sink.session.snapshot.cursor.Valid() || liveVerifiedEvidenceRunBindings() != before {
		t.Fatalf("public runner authority preflight leaked evidence authority: session=%+v live=%d/%d", sink.session, liveVerifiedEvidenceRunBindings(), before)
	}
	if !reflect.DeepEqual(observer.transitions, []RunnerState{StateVerifyTrust, StateLoadBundle, StateConnect, StateLocked, StateMigrate}) {
		t.Fatalf("public runner authority preflight ordering=%v", observer.transitions)
	}
	bundle, err := LoadRuntimeBundle(raw, decision)
	if err != nil {
		t.Fatalf("reload public runner fixture: %v", err)
	}
	key, err := bundle.Manifest.SchemaBundle.AdvisoryLock.Key()
	if err != nil {
		t.Fatalf("public runner advisory key: %v", err)
	}
	var acquired bool
	if err := admin.QueryRow(ctx, "SELECT pg_catalog.pg_try_advisory_lock($1)", key).Scan(&acquired); err != nil || !acquired {
		t.Fatalf("public runner left the signed advisory lock held: acquired=%v err=%v", acquired, err)
	}
	var unlocked bool
	if err := admin.QueryRow(ctx, "SELECT pg_catalog.pg_advisory_unlock($1)", key).Scan(&unlocked); err != nil || !unlocked {
		t.Fatalf("release public runner cleanup probe lock: unlocked=%v err=%v", unlocked, err)
	}
	assertPostgresSchemaAbsent(t, ctx, admin)
}

func (contract VerifiedAuthorityContract) expectedProjectionForTest(phase AuthorityPhase) AuthorityProjection {
	switch phase {
	case AuthorityPhaseConnectedSession:
		return contract.expected.ConnectedSession
	case AuthorityPhaseMigrationRole:
		return contract.expected.MigrationRole
	default:
		return contract.expected.MigrationTransaction
	}
}

func projectPostgresPreconditionIdle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, verified VerifiedSchemaBundleScope, condition CatalogPrecondition, expected CatalogStateProjection, environment postgresProjectionEnvironment) Digest {
	t.Helper()
	snapshot, err := BeginIdleReadSnapshot(ctx, pool, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin catalog snapshot: %v", err)
	}
	defer func() {
		if err := snapshot.RollbackAndRelease(context.Background()); err != nil {
			t.Fatalf("rollback catalog snapshot: %v", err)
		}
	}()
	projector, err := NewPGProjector(ctx, snapshot)
	if err != nil {
		t.Fatalf("probe catalog capability: %v", err)
	}
	result, err := projector.ProjectPrecondition(ctx, snapshot, verified, condition)
	if err != nil {
		t.Fatalf("project checked-in predecessor fixture after query_count=%d: %v", snapshot.projectionStats().QueryCount, err)
	}
	want, err := expected.ComputeDigest()
	if err != nil || result.Digest != want {
		t.Fatalf("predecessor digest differs from checked-in fixture: got=%s want=%s err=%v", result.Digest, want, err)
	}
	if result.Metadata.QueryCount != 1 && result.Projection.Absent != nil || result.Metadata.QueryCount != 3 && result.Projection.Present != nil {
		t.Fatalf("unexpected predecessor query count: %+v", result.Metadata)
	}
	assertPostgresSnapshotMetadata(t, result.Metadata.Snapshot, environment, AuthorityPhaseConnectedSession, IdleReadSnapshot)
	return result.Digest
}

func projectPostgresCatalogIdle(t *testing.T, ctx context.Context, pool *pgxpool.Pool, contract VerifiedCatalogContract, environment postgresProjectionEnvironment) Digest {
	t.Helper()
	snapshot, err := BeginIdleReadSnapshot(ctx, pool, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin full catalog idle snapshot: %v", err)
	}
	defer func() {
		if err := snapshot.RollbackAndRelease(context.Background()); err != nil {
			t.Fatalf("rollback full catalog idle snapshot: %v", err)
		}
	}()
	projector, err := NewPGProjector(ctx, snapshot)
	if err != nil {
		t.Fatalf("probe full catalog idle capability: %v", err)
	}
	result, err := projector.ProjectCatalog(ctx, snapshot, contract, contract.Scope())
	if err != nil {
		if IsCode(err, CodeCatalogDrift) {
			structure, readErr := projector.readCatalogStructureWithExpressions(ctx, snapshot, contract.Scope(), contract.defaultACLOwners, contract.objectCreatorClosure)
			if readErr == nil {
				body, bodyErr := structure.completeBody(environment.Major)
				if bodyErr == nil {
					actualKey, _ := canonicalContractKey(CatalogProjection{SchemaHead: contract.ExpectedProjection().SchemaHead, Body: body})
					expectedKey, _ := canonicalContractKey(contract.ExpectedProjection())
					t.Fatalf("project checked-in full catalog through idle snapshot: %v; %s", err, firstProjectionDifference(actualKey, expectedKey))
				}
			}
		}
		t.Fatalf("project checked-in full catalog through idle snapshot: %v", err)
	}
	assertPostgresCatalogProjectionResult(t, result, contract, environment, IdleReadSnapshot)
	return result.Digest
}

func firstProjectionDifference(actual, expected string) string {
	limit := len(actual)
	if len(expected) < limit {
		limit = len(expected)
	}
	index := 0
	for index < limit && actual[index] == expected[index] {
		index++
	}
	start := index - 96
	if start < 0 {
		start = 0
	}
	actualEnd := index + 192
	if actualEnd > len(actual) {
		actualEnd = len(actual)
	}
	expectedEnd := index + 192
	if expectedEnd > len(expected) {
		expectedEnd = len(expected)
	}
	return fmt.Sprintf("first_diff=%d actual_digest=%s expected_digest=%s actual=%q expected=%q", index, DigestBytes([]byte(actual)), DigestBytes([]byte(expected)), actual[start:actualEnd], expected[start:expectedEnd])
}

func projectPostgresCatalogBorrowed(t *testing.T, ctx context.Context, environment postgresProjectionEnvironment, contract VerifiedCatalogContract) Digest {
	t.Helper()
	session, err := (PGXConnector{}).Connect(ctx, environment.MigrationURL)
	if err != nil {
		t.Fatalf("connect full catalog borrowed session: %v", err)
	}
	defer func() { _ = session.Close(context.Background()) }()
	policy := ExecutionPolicy{StatementTimeoutMS: 5000, LockTimeoutMS: 1000, IdleInTransactionSessionTimeoutMS: 60000}
	if err := session.SetRoleAndSettings(ctx, policy); err != nil {
		t.Fatalf("configure full catalog borrowed session: %v", err)
	}
	const advisoryKey int64 = 0x10203040506070a
	if err := session.AcquireAdvisoryLock(ctx, advisoryKey); err != nil {
		t.Fatalf("acquire full catalog borrowed lock: %v", err)
	}
	transaction, err := session.BeginMigration(ctx)
	if err != nil {
		t.Fatalf("begin full catalog borrowed transaction: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	scope := contract.Scope()
	snapshot, err := BorrowMigrationProjectionSnapshot(ctx, transaction, *scope.SchemaHead, nil)
	if err != nil {
		t.Fatalf("borrow full catalog migration snapshot: %v", err)
	}
	projector, err := NewPGProjector(ctx, snapshot)
	if err != nil {
		t.Fatalf("probe full catalog borrowed capability: %v", err)
	}
	result, err := projector.ProjectCatalog(ctx, snapshot, contract, scope)
	if err != nil {
		t.Fatalf("project checked-in full catalog through borrowed snapshot: %v", err)
	}
	assertPostgresCatalogProjectionResult(t, result, contract, environment, MigrationSnapshot)
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback full catalog borrowed transaction: %v", err)
	}
	if err := session.UnlockAndReset(ctx, advisoryKey); err != nil {
		t.Fatalf("release full catalog borrowed lock/session: %v", err)
	}
	return result.Digest
}

func assertPostgresCatalogProjectionResult(t *testing.T, result ProjectionResult[CatalogProjection], contract VerifiedCatalogContract, environment postgresProjectionEnvironment, mode SnapshotMode) {
	t.Helper()
	expected := contract.ExpectedProjection()
	want, err := digestProjectionWrapper(CatalogProjectionDigestDomain, expected)
	if err != nil || result.Digest != want || !runnerCanonicalEqual(result.Projection, expected) {
		t.Fatalf("full catalog differs from checked-in signed subject: got=%s want=%s err=%v", result.Digest, want, err)
	}
	if result.Metadata.ProjectionKind != ProjectionKindCatalog || result.Metadata.DigestDomain != CatalogProjectionDigestDomain || result.Metadata.AdapterProfile != PostgreSQLCatalogAdapter || result.Metadata.VerifiedSubjectDigest != contract.SubjectDigest() || result.Metadata.Scope == nil || !equalProjectionScopes(*result.Metadata.Scope, contract.Scope()) || result.Metadata.QueryCount == 0 || result.Metadata.RowCount == 0 || result.Metadata.TotalBytes == 0 {
		t.Fatalf("full catalog result metadata is incomplete: %+v", result.Metadata)
	}
	phase := AuthorityPhaseConnectedSession
	if mode == MigrationSnapshot {
		phase = AuthorityPhaseMigrationTransaction
	}
	assertPostgresSnapshotMetadata(t, result.Metadata.Snapshot, environment, phase, mode)
}

func projectPostgresBorrowedTransaction(t *testing.T, ctx context.Context, environment postgresProjectionEnvironment, authority VerifiedAuthorityContract, verifiedSchema VerifiedSchemaBundleScope, condition CatalogPrecondition, expectedPresent CatalogStateProjection) (Digest, Digest) {
	t.Helper()
	session, err := (PGXConnector{}).Connect(ctx, environment.MigrationURL)
	if err != nil {
		t.Fatalf("connect dedicated migration session: %v", err)
	}
	defer func() { _ = session.Close(context.Background()) }()
	policy := ExecutionPolicy{StatementTimeoutMS: 5000, LockTimeoutMS: 1000, IdleInTransactionSessionTimeoutMS: 60000}
	if err := session.SetRoleAndSettings(ctx, policy); err != nil {
		t.Fatalf("configure migration role/session: %v", err)
	}
	const advisoryKey int64 = 0x102030405060708
	if err := session.AcquireAdvisoryLock(ctx, advisoryKey); err != nil {
		t.Fatalf("acquire migration advisory lock: %v", err)
	}
	transaction, err := session.BeginMigration(ctx)
	if err != nil {
		t.Fatalf("begin migration transaction: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	boundary, err := transaction.Boundary(ctx, advisoryKey)
	if err != nil || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		t.Fatalf("migration transaction boundary before projection: state=%+v err=%v", boundary, err)
	}
	snapshot, err := BorrowMigrationProjectionSnapshot(ctx, transaction, "000001", nil)
	if err != nil {
		t.Fatalf("borrow migration projection snapshot: %v", err)
	}
	assertPostgresSnapshotMetadata(t, snapshot.Metadata(), environment, AuthorityPhaseMigrationTransaction, MigrationSnapshot)
	projector, err := NewPGProjector(ctx, snapshot)
	if err != nil {
		t.Fatalf("probe borrowed migration capability: %v", err)
	}
	authorityResult, err := projector.ProjectAuthority(ctx, snapshot, authority, AuthorityPhaseMigrationTransaction)
	if err != nil {
		t.Fatalf("project migration transaction authority: %v", err)
	}
	wantAuthority, err := digestProjectionWrapper(AuthorityProjectionDigestDomain, authority.expected.MigrationTransaction)
	if err != nil || authorityResult.Digest != wantAuthority {
		t.Fatalf("migration transaction authority digest differs: got=%s want=%s err=%v", authorityResult.Digest, wantAuthority, err)
	}

	if _, err := transaction.Exec(ctx, "CREATE SCHEMA cloud_agents AUTHORIZATION "+MigrationOwnerRole); err != nil {
		t.Fatalf("create schema inside borrowed migration transaction: %v", err)
	}
	if _, err := transaction.Exec(ctx, "ALTER DEFAULT PRIVILEGES FOR ROLE "+MigrationOwnerRole+" IN SCHEMA cloud_agents GRANT INSERT ON TABLES TO "+postgresProjectionLogin); err != nil {
		t.Fatalf("create scoped default ACL inside borrowed migration transaction: %v", err)
	}
	var transactionIDBefore, transactionIDAfter string
	if err := transaction.QueryRow(ctx, "SELECT pg_catalog.pg_current_xact_id()::pg_catalog.text").Scan(&transactionIDBefore); err != nil {
		t.Fatalf("read migration transaction identity before projection: %v", err)
	}
	presentResult, err := projector.ProjectPrecondition(ctx, snapshot, verifiedSchema, condition)
	if err != nil {
		t.Fatalf("borrowed projector did not see its own schema/default-ACL writes: %v", err)
	}
	if err := transaction.QueryRow(ctx, "SELECT pg_catalog.pg_current_xact_id()::pg_catalog.text").Scan(&transactionIDAfter); err != nil {
		t.Fatalf("read migration transaction identity after projection: %v", err)
	}
	if transactionIDBefore == "" || transactionIDBefore != transactionIDAfter {
		t.Fatalf("projection changed transaction identity: before=%q after=%q", transactionIDBefore, transactionIDAfter)
	}
	wantPresent, err := expectedPresent.ComputeDigest()
	if err != nil || presentResult.Digest != wantPresent {
		t.Fatalf("borrowed own-write projection differs from checked-in fixture: got=%s want=%s err=%v", presentResult.Digest, wantPresent, err)
	}
	boundary, err = transaction.Boundary(ctx, advisoryKey)
	if err != nil || boundary.TxStatus != 'T' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		t.Fatalf("borrowed projector changed transaction ownership: state=%+v err=%v", boundary, err)
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback borrowed migration transaction: %v", err)
	}
	boundary, err = session.Boundary(ctx, advisoryKey)
	if err != nil || boundary.TxStatus != 'I' || boundary.CurrentUser != MigrationOwnerRole || !boundary.LockHeld {
		t.Fatalf("migration session not reusable after rollback: state=%+v err=%v", boundary, err)
	}
	if err := session.UnlockAndReset(ctx, advisoryKey); err != nil {
		t.Fatalf("unlock/reset migration session: %v", err)
	}
	return authorityResult.Digest, presentResult.Digest
}

func assertPostgresCapabilities(t *testing.T, projector *PGProjector, environment postgresProjectionEnvironment) {
	t.Helper()
	wantOptions := environment.Major >= 16
	if projector.major != environment.Major || projector.capabilities.ServerVersionNum != environment.ServerVersion ||
		projector.capabilities.MembershipInheritOption != wantOptions || projector.capabilities.MembershipSetOption != wantOptions ||
		projector.capabilities.DatabaseICURules != (environment.Major >= 16) || projector.capabilities.DatabaseLocaleUnifiedColumn != (environment.Major >= 17) {
		t.Fatalf("PG%d capability profile differs: %+v", environment.Major, projector.capabilities)
	}
}

func assertPostgresSnapshotMetadata(t *testing.T, metadata SnapshotMetadata, environment postgresProjectionEnvironment, phase AuthorityPhase, mode SnapshotMode) {
	t.Helper()
	if metadata.PostgresMajor != environment.Major || metadata.ServerVersionNum != environment.ServerVersion || metadata.DatabaseName != postgresProjectionDatabase || metadata.SessionUser != postgresProjectionLogin || metadata.AuthorityPhase != phase || metadata.TxStatus != "T" || metadata.Deferrable {
		t.Fatalf("snapshot identity/profile differs: %+v", metadata)
	}
	if mode == IdleReadSnapshot && (metadata.Mode != IdleReadSnapshot || metadata.Ownership != OwnedIdleSnapshot || metadata.IsolationLevel != "repeatable_read" || metadata.AccessMode != "read_only") {
		t.Fatalf("owned idle snapshot profile differs: %+v", metadata)
	}
	if mode == MigrationSnapshot && (metadata.Mode != MigrationSnapshot || metadata.Ownership != BorrowedMigrationSnapshot || metadata.IsolationLevel != "serializable" || metadata.AccessMode != "read_write") {
		t.Fatalf("borrowed migration snapshot profile differs: %+v", metadata)
	}
}

func restorePostgresProjectionBaseline(t *testing.T, ctx context.Context, admin *pgx.Conn, major uint16) {
	t.Helper()
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	execPostgresStatements(t, ctx, admin,
		"RESET ROLE",
		"DROP SCHEMA IF EXISTS cloud_agents CASCADE",
		"ALTER DATABASE "+postgresProjectionDatabase+" OWNER TO "+postgresProjectionDatabaseOwner,
		"REVOKE ALL PRIVILEGES ON DATABASE "+postgresProjectionDatabase+" FROM PUBLIC",
		"REVOKE ALL PRIVILEGES ON DATABASE "+postgresProjectionDatabase+" FROM "+MigrationOwnerRole,
		"GRANT CONNECT ON DATABASE "+postgresProjectionDatabase+" TO PUBLIC",
		"GRANT CREATE ON DATABASE "+postgresProjectionDatabase+" TO "+MigrationOwnerRole,
		"ALTER ROLE "+postgresProjectionLogin+" NOINHERIT",
		"REVOKE "+MigrationOwnerRole+" FROM "+postgresProjectionLogin,
	)
	grant := "GRANT " + MigrationOwnerRole + " TO " + postgresProjectionLogin
	if major >= 16 {
		grant += " WITH ADMIN FALSE, INHERIT FALSE, SET TRUE"
	}
	execPostgresStatements(t, ctx, admin, grant,
		"ALTER DEFAULT PRIVILEGES FOR ROLE "+MigrationOwnerRole+" GRANT SELECT ON TABLES TO "+postgresProjectionLogin,
	)
	if major >= 17 {
		execPostgresStatements(t, ctx, admin, "ALTER DEFAULT PRIVILEGES FOR ROLE "+MigrationOwnerRole+" REVOKE MAINTAIN ON TABLES FROM "+MigrationOwnerRole)
	}
}

func createPostgresProjectionSchema(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	execPostgresStatements(t, ctx, admin,
		"CREATE SCHEMA cloud_agents AUTHORIZATION "+MigrationOwnerRole,
		"ALTER DEFAULT PRIVILEGES FOR ROLE "+MigrationOwnerRole+" IN SCHEMA cloud_agents GRANT INSERT ON TABLES TO "+postgresProjectionLogin,
	)
}

func createPostgresRepresentativeCatalog(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	execPostgresStatements(t, ctx, admin, `SET ROLE cloud_agents_migration_owner`, `
CREATE TABLE cloud_agents.probe (
 value integer NOT NULL DEFAULT 1,
 generated_value integer GENERATED ALWAYS AS (value + 1) STORED,
 CONSTRAINT probe_value_check CHECK (value > 0)
);
CREATE INDEX probe_value_expression_idx ON cloud_agents.probe((value + 1)) WHERE value > 0;
ALTER TABLE cloud_agents.probe ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.probe FORCE ROW LEVEL SECURITY;
CREATE POLICY probe_public ON cloud_agents.probe TO PUBLIC USING (value > 0);
CREATE FUNCTION cloud_agents.probe_touch_fn() RETURNS trigger
LANGUAGE plpgsql AS $body$ BEGIN RETURN NEW; END $body$;
CREATE FUNCTION cloud_agents.add_one(value integer, delta integer DEFAULT 1)
RETURNS integer LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $body$ SELECT value + delta $body$;
CREATE TRIGGER probe_touch BEFORE UPDATE ON cloud_agents.probe
FOR EACH ROW WHEN (NEW.value > 0) EXECUTE FUNCTION cloud_agents.probe_touch_fn()
`, `RESET ROLE`)
}

func postgresRepresentativeCatalogScope() ProjectionScope {
	head := "900001"
	declared := []ObjectIdentityProjection{
		{Schema: &SchemaObjectIdentity{Kind: "schema", Name: projectionTargetSchema}},
		{Relation: &RelationObjectIdentity{Kind: "relation", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "probe"}}},
		{Index: &IndexObjectIdentity{Kind: "index", Identity: TypeIdentity{Schema: projectionTargetSchema, Name: "probe_value_expression_idx"}, Relation: TypeIdentity{Schema: projectionTargetSchema, Name: "probe"}}},
		{Policy: &PolicyObjectIdentity{Kind: "policy", Relation: TypeIdentity{Schema: projectionTargetSchema, Name: "probe"}, Name: "probe_public"}},
		{Function: &SQLObjectIdentity{Kind: "function", Identity: SQLIdentity{Schema: projectionTargetSchema, Name: "probe_touch_fn", Arguments: []TypeIdentity{}}}},
		{Function: &SQLObjectIdentity{Kind: "function", Identity: SQLIdentity{Schema: projectionTargetSchema, Name: "add_one", Arguments: []TypeIdentity{{Schema: "pg_catalog", Name: "int4"}, {Schema: "pg_catalog", Name: "int4"}}}}},
		{Trigger: &TriggerObjectIdentity{Kind: "trigger", Relation: TypeIdentity{Schema: projectionTargetSchema, Name: "probe"}, Name: "probe_touch"}},
	}
	sort.Slice(declared, func(left, right int) bool {
		leftKey, _ := canonicalContractKey(declared[left])
		rightKey, _ := canonicalContractKey(declared[right])
		return leftKey < rightKey
	})
	return ProjectionScope{ScopeKind: "final", SchemaHead: &head, DeclaredObjects: declared}
}

func assertPostgresSchemaAbsent(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname='cloud_agents')").Scan(&exists); err != nil || exists {
		t.Fatalf("borrowed transaction rollback leaked target schema: exists=%t err=%v", exists, err)
	}
}

func testPostgresAuthorityDrift(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool, contract VerifiedAuthorityContract, environment postgresProjectionEnvironment) {
	t.Helper()
	t.Run("database-owner", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "ALTER DATABASE "+postgresProjectionDatabase+" OWNER TO postgres")
		defer execPostgresCleanupStatements(t, admin, "ALTER DATABASE "+postgresProjectionDatabase+" OWNER TO "+postgresProjectionDatabaseOwner)
		assertPostgresAuthorityError(t, ctx, pool, contract, environment, CodeAuthorityDrift)
	})
	t.Run("database-acl", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "GRANT TEMPORARY ON DATABASE "+postgresProjectionDatabase+" TO "+MigrationOwnerRole)
		defer execPostgresCleanupStatements(t, admin, "REVOKE TEMPORARY ON DATABASE "+postgresProjectionDatabase+" FROM "+MigrationOwnerRole)
		assertPostgresAuthorityError(t, ctx, pool, contract, environment, CodeAuthorityDrift)
	})
	t.Run("role-attribute", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "ALTER ROLE "+postgresProjectionLogin+" INHERIT")
		defer execPostgresCleanupStatements(t, admin, "ALTER ROLE "+postgresProjectionLogin+" NOINHERIT")
		assertPostgresAuthorityError(t, ctx, pool, contract, environment, CodeAuthorityDrift)
	})
	t.Run("membership", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "REVOKE "+MigrationOwnerRole+" FROM "+postgresProjectionLogin)
		grant := "GRANT " + MigrationOwnerRole + " TO " + postgresProjectionLogin + " WITH ADMIN OPTION"
		if environment.Major >= 16 {
			grant = "GRANT " + MigrationOwnerRole + " TO " + postgresProjectionLogin + " WITH ADMIN TRUE, INHERIT TRUE, SET TRUE"
		}
		execPostgresStatements(t, ctx, admin, grant)
		restoreGrant := "GRANT " + MigrationOwnerRole + " TO " + postgresProjectionLogin
		if environment.Major >= 16 {
			restoreGrant += " WITH ADMIN FALSE, INHERIT FALSE, SET TRUE"
		}
		defer execPostgresCleanupStatements(t, admin,
			"REVOKE "+MigrationOwnerRole+" FROM "+postgresProjectionLogin,
			restoreGrant,
		)
		assertPostgresAuthorityError(t, ctx, pool, contract, environment, CodeAuthorityDrift)
	})
}

func assertPostgresAuthorityError(t *testing.T, ctx context.Context, pool *pgxpool.Pool, contract VerifiedAuthorityContract, environment postgresProjectionEnvironment, code ErrorCode) {
	t.Helper()
	snapshot, err := BeginIdleReadSnapshot(ctx, pool, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin authority drift snapshot: %v", err)
	}
	defer func() { _ = snapshot.RollbackAndRelease(context.Background()) }()
	projector, err := NewPGProjector(ctx, snapshot)
	if err != nil {
		t.Fatalf("probe authority drift capability: %v", err)
	}
	assertPostgresCapabilities(t, projector, environment)
	if _, err := projector.ProjectAuthority(ctx, snapshot, contract, AuthorityPhaseConnectedSession); !IsCode(err, code) {
		t.Fatalf("authority drift code=%s err=%v", code, err)
	}
}

func testPostgresCatalogDrift(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool, verified VerifiedSchemaBundleScope, condition CatalogPrecondition, environment postgresProjectionEnvironment) {
	t.Helper()
	t.Run("schema-owner", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "ALTER SCHEMA cloud_agents OWNER TO "+postgresProjectionDatabaseOwner)
		defer execPostgresCleanupStatements(t, admin, "ALTER SCHEMA cloud_agents OWNER TO "+MigrationOwnerRole)
		assertPostgresCatalogError(t, ctx, pool, verified, condition, environment, CodeAuthorityDrift)
	})
	t.Run("schema-acl", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "GRANT USAGE ON SCHEMA cloud_agents TO "+postgresProjectionLogin)
		defer execPostgresCleanupStatements(t, admin, "REVOKE USAGE ON SCHEMA cloud_agents FROM "+postgresProjectionLogin)
		assertPostgresCatalogError(t, ctx, pool, verified, condition, environment, CodeCatalogDrift)
	})
	t.Run("default-acl", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin,
			"ALTER DEFAULT PRIVILEGES FOR ROLE "+MigrationOwnerRole+" IN SCHEMA cloud_agents GRANT UPDATE ON TABLES TO "+postgresProjectionLogin,
		)
		defer execPostgresCleanupStatements(t, admin,
			"ALTER DEFAULT PRIVILEGES FOR ROLE "+MigrationOwnerRole+" IN SCHEMA cloud_agents REVOKE UPDATE ON TABLES FROM "+postgresProjectionLogin,
		)
		assertPostgresCatalogError(t, ctx, pool, verified, condition, environment, CodeCatalogDrift)
	})
	t.Run("unknown-object", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "CREATE TABLE cloud_agents.unbound_relation (id bigint)")
		defer execPostgresCleanupStatements(t, admin, "DROP TABLE cloud_agents.unbound_relation")
		assertPostgresCatalogError(t, ctx, pool, verified, condition, environment, CodeProjectionUnknownObject)
	})
}

func assertPostgresCatalogError(t *testing.T, ctx context.Context, pool *pgxpool.Pool, verified VerifiedSchemaBundleScope, condition CatalogPrecondition, environment postgresProjectionEnvironment, code ErrorCode) {
	t.Helper()
	snapshot, err := BeginIdleReadSnapshot(ctx, pool, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin catalog drift snapshot: %v", err)
	}
	defer func() { _ = snapshot.RollbackAndRelease(context.Background()) }()
	projector, err := NewPGProjector(ctx, snapshot)
	if err != nil {
		t.Fatalf("probe catalog drift capability: %v", err)
	}
	assertPostgresCapabilities(t, projector, environment)
	if _, err := projector.ProjectPrecondition(ctx, snapshot, verified, condition); !IsCode(err, code) {
		t.Fatalf("catalog drift code=%s err=%v", code, err)
	}
}

func testPostgresFullCatalogDrift(t *testing.T, ctx context.Context, admin *pgx.Conn, pool *pgxpool.Pool, contract VerifiedCatalogContract, environment postgresProjectionEnvironment) {
	t.Helper()
	t.Run("expression", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "ALTER TABLE cloud_agents.probe ALTER COLUMN value SET DEFAULT 2")
		defer execPostgresCleanupStatements(t, admin, "ALTER TABLE cloud_agents.probe ALTER COLUMN value SET DEFAULT 1")
		assertPostgresFullCatalogError(t, ctx, pool, contract, environment, CodeCatalogDrift)
	})
	t.Run("function-source", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, `CREATE OR REPLACE FUNCTION cloud_agents.add_one(value integer, delta integer DEFAULT 1) RETURNS integer LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $body$ SELECT value + delta + 1 $body$`)
		defer execPostgresCleanupStatements(t, admin, `CREATE OR REPLACE FUNCTION cloud_agents.add_one(value integer, delta integer DEFAULT 1) RETURNS integer LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $body$ SELECT value + delta $body$`)
		assertPostgresFullCatalogError(t, ctx, pool, contract, environment, CodeCatalogDrift)
	})
	t.Run("creator-closure", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "GRANT CREATE ON SCHEMA cloud_agents TO "+postgresProjectionDatabaseOwner)
		defer execPostgresCleanupStatements(t, admin, "REVOKE CREATE ON SCHEMA cloud_agents FROM "+postgresProjectionDatabaseOwner)
		assertPostgresFullCatalogError(t, ctx, pool, contract, environment, CodeAuthorityDrift)
	})
	t.Run("unknown-object", func(t *testing.T) {
		execPostgresStatements(t, ctx, admin, "CREATE TABLE cloud_agents.unbound_relation (id bigint)")
		defer execPostgresCleanupStatements(t, admin, "DROP TABLE cloud_agents.unbound_relation")
		assertPostgresFullCatalogError(t, ctx, pool, contract, environment, CodeProjectionUnknownObject)
	})
}

func assertPostgresFullCatalogError(t *testing.T, ctx context.Context, pool *pgxpool.Pool, contract VerifiedCatalogContract, environment postgresProjectionEnvironment, code ErrorCode) {
	t.Helper()
	snapshot, err := BeginIdleReadSnapshot(ctx, pool, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin full catalog drift snapshot: %v", err)
	}
	defer func() { _ = snapshot.RollbackAndRelease(context.Background()) }()
	projector, err := NewPGProjector(ctx, snapshot)
	if err != nil {
		t.Fatalf("probe full catalog drift capability: %v", err)
	}
	assertPostgresCapabilities(t, projector, environment)
	if _, err := projector.ProjectCatalog(ctx, snapshot, contract, contract.Scope()); !IsCode(err, code) {
		t.Fatalf("full catalog drift code=%s err=%v", code, err)
	}
}

func testPostgresCanceledSnapshotCleanup(t *testing.T, ctx context.Context, pool *pgxpool.Pool, environment postgresProjectionEnvironment) {
	t.Helper()
	snapshot, err := BeginIdleReadSnapshot(ctx, pool, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin canceled snapshot: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := snapshot.queryProjection(canceled, projectionQueryCapability); !IsCode(err, CodeProjectionCatalogQueryFailed) {
		t.Fatalf("canceled projection query did not fail closed: %v", err)
	}
	if err := snapshot.RollbackAndRelease(context.Background()); err != nil {
		t.Fatalf("canceled snapshot rollback/release: %v", err)
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("reacquire canceled snapshot connection: %v", err)
	}
	defer connection.Release()
	var isolation, readOnly, deferrable string
	if err := connection.QueryRow(ctx, "SELECT current_setting('transaction_isolation'), current_setting('transaction_read_only'), current_setting('transaction_deferrable')").Scan(&isolation, &readOnly, &deferrable); err != nil {
		t.Fatalf("read reusable connection profile: %v", err)
	}
	if connection.Conn().PgConn().TxStatus() != 'I' || isolation != "read committed" || readOnly != "off" || deferrable != "off" {
		t.Fatalf("canceled snapshot leaked transaction-local state: status=%c isolation=%s read_only=%s deferrable=%s major=%d", connection.Conn().PgConn().TxStatus(), isolation, readOnly, deferrable, environment.Major)
	}
}

func testPostgresTerminatedBackendHijack(t *testing.T, ctx context.Context, admin *pgx.Conn, environment postgresProjectionEnvironment) {
	t.Helper()
	pool := newPostgresProjectionPool(t, ctx, environment.MigrationURL, false)
	defer pool.Close()
	snapshot, err := BeginIdleReadSnapshot(ctx, pool, AuthorityPhaseConnectedSession)
	if err != nil {
		t.Fatalf("begin backend termination snapshot: %v", err)
	}
	defer func() { _ = snapshot.RollbackAndRelease(context.Background()) }()
	owned, ok := snapshot.(*ownedIdleProjectionSnapshot)
	if !ok {
		t.Fatalf("owned snapshot concrete type=%T", snapshot)
	}
	connection, ok := owned.connection.(*pgxIdleSnapshotConnection)
	if !ok {
		t.Fatalf("owned snapshot connection type=%T", owned.connection)
	}
	terminatedPID := connection.connection.Conn().PgConn().PID()
	var terminated bool
	if err := admin.QueryRow(ctx, "SELECT pg_catalog.pg_terminate_backend($1)", terminatedPID).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate owned snapshot backend pid=%d terminated=%t err=%v", terminatedPID, terminated, err)
	}
	if err := snapshot.RollbackAndRelease(context.Background()); !IsCode(err, CodeProjectionSnapshotInvalid) {
		t.Fatalf("terminated backend did not trigger hijack/close: %v", err)
	}
	replacement, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire replacement after hijack: %v", err)
	}
	replacementPID := replacement.Conn().PgConn().PID()
	status := replacement.Conn().PgConn().TxStatus()
	replacement.Release()
	if replacementPID == terminatedPID || status != 'I' {
		t.Fatalf("terminated connection returned to pool: old_pid=%d replacement_pid=%d status=%c", terminatedPID, replacementPID, status)
	}
}

func execPostgresStatements(t *testing.T, ctx context.Context, connection *pgx.Conn, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if _, err := connection.Exec(ctx, statement); err != nil {
			t.Fatalf("execute local projection matrix statement %q: %v", statement, err)
		}
	}
}

func execPostgresCleanupStatements(t *testing.T, connection *pgx.Conn, statements ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	execPostgresStatements(t, ctx, connection, statements...)
}
