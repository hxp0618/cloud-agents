-- A2.4 append-only compatibility/recovery kernel.  This migration binds the
-- generated compatibility/recovery registry to durable PostgreSQL shape only.
-- It deliberately exposes no mutation function, service claim, HTTP surface,
-- provider call, worker effect, or production Gate authority.

CREATE FUNCTION cloud_agents.compatibility_recovery_registry_digest()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:9df9dcf4c9e62cd95b43be362bf5a332bf9637ca881f16fbd25486ad0792f72d'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_state_machine_digest()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:5fb7f076c40aed31d5309a4de6aa2a66b93f3d560a535ecf992dc1f817d8f408'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_policy_digest()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:804ee0280ab5c98a48989abf511659d2a6f801fa5201617c3e436f848dfdc11d'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_profile_digest(
    p_profile_id text
)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT CASE p_profile_id
        WHEN 'backfill/v1'
            THEN 'sha256:779791352f9ba77f1f75c3cd6e5b4a846ee00687217eb3489ec8877513809047'::text
        WHEN 'live-instance/v1'
            THEN 'sha256:aeb12441bc83a110047a1a69a413d2672cf5ba8c82747d52a842ab91c4840790'::text
        WHEN 'migration-preflight/v1'
            THEN 'sha256:0ef86c85d7878202ac16f06c6b32a7bd84d642433a7098a25fc09b5f7f8599ba'::text
        WHEN 'restore-evidence/v1'
            THEN 'sha256:d095186e6f70205f9c842acc8e7232ff658c4aecbed06436ad91532e6cf4042e'::text
        WHEN 'retirement-receipt/v1'
            THEN 'sha256:cf2e57dcf51bfea35e7ca82875acb04225e5a050fcf3d394cb6f1bc457d2a3ac'::text
        ELSE NULL::text
    END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_profile_is_registered(
    p_profile_id text,
    p_profile_digest text
)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT COALESCE(
        p_profile_digest IS NOT NULL
        AND p_profile_digest = cloud_agents.compatibility_recovery_profile_digest(p_profile_id),
        false
    )
$cloud_agents_function$;

CREATE TABLE cloud_agents.workload_database_principals (
    database_principal text PRIMARY KEY,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    service_kind text NOT NULL,
    instance_id text NOT NULL,
    incarnation bigint NOT NULL,
    rollout_generation bigint NOT NULL,
    writer_epoch bigint NOT NULL,
    can_register_instance boolean NOT NULL DEFAULT false,
    can_heartbeat_instance boolean NOT NULL DEFAULT false,
    can_retire_instance boolean NOT NULL DEFAULT false,
    state text NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    CONSTRAINT workload_database_principals_principal
        CHECK (cloud_agents.is_valid_identifier(database_principal)),
    CONSTRAINT workload_database_principals_registry
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest()),
    CONSTRAINT workload_database_principals_service_kind
        CHECK (cloud_agents.is_valid_identifier(service_kind)),
    CONSTRAINT workload_database_principals_instance
        CHECK (cloud_agents.is_valid_identifier(instance_id)
            AND incarnation >= 1
            AND rollout_generation >= 1
            AND writer_epoch >= 1),
    CONSTRAINT workload_database_principals_state
        CHECK (state IN ('active', 'expired', 'revoked')),
    CONSTRAINT workload_database_principals_time
        CHECK (updated_at >= created_at AND expires_at > created_at)
);

CREATE TABLE cloud_agents.migration_backfills (
    migration_id text PRIMARY KEY,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    state text NOT NULL,
    phase text NOT NULL,
    cursor text NOT NULL,
    digest text NOT NULL,
    count bigint NOT NULL DEFAULT 0,
    batch_generation bigint NOT NULL DEFAULT 0,
    committed_at timestamptz,
    stable_error_code text,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    CONSTRAINT migration_backfills_migration_id
        CHECK (migration_id ~ '^[0-9]{6}$'),
    CONSTRAINT migration_backfills_registry
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest()
            AND profile_id = 'backfill/v1'
            AND cloud_agents.compatibility_recovery_profile_is_registered(
                profile_id, profile_digest)),
    CONSTRAINT migration_backfills_state
        CHECK (state IN ('failed', 'paused', 'pending', 'running', 'succeeded')),
    CONSTRAINT migration_backfills_phase
        CHECK (cloud_agents.is_valid_identifier(phase)),
    CONSTRAINT migration_backfills_cursor
        CHECK (pg_catalog.octet_length(cursor) BETWEEN 1 AND 2048
            AND digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT migration_backfills_counts
        CHECK (count >= 0 AND batch_generation >= 0),
    CONSTRAINT migration_backfills_error
        CHECK (stable_error_code IS NULL
            OR cloud_agents.is_valid_identifier(stable_error_code)),
    CONSTRAINT migration_backfills_time
        CHECK (updated_at >= created_at
            AND (committed_at IS NULL OR committed_at >= created_at))
);

CREATE TABLE cloud_agents.schema_restore_evidence (
    drill_id text PRIMARY KEY,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    state text NOT NULL,
    scope text NOT NULL,
    postgres_major integer NOT NULL,
    ledger_checksum text NOT NULL,
    target_schema_bundle_digest text NOT NULL,
    target_phase text NOT NULL,
    restore_point_digest text NOT NULL,
    evidence_digest text NOT NULL UNIQUE,
    drill_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    CONSTRAINT schema_restore_evidence_drill
        CHECK (cloud_agents.is_valid_identifier(drill_id)),
    CONSTRAINT schema_restore_evidence_registry
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest()
            AND profile_id = 'restore-evidence/v1'
            AND cloud_agents.compatibility_recovery_profile_is_registered(
                profile_id, profile_digest)),
    CONSTRAINT schema_restore_evidence_state
        CHECK (state IN ('drill_verified', 'eligible', 'invalidated', 'recorded')),
    CONSTRAINT schema_restore_evidence_scope
        CHECK (scope = 'local_logical_backup_restore_and_preflight'),
    CONSTRAINT schema_restore_evidence_postgres
        CHECK (postgres_major BETWEEN 15 AND 17),
    CONSTRAINT schema_restore_evidence_digests
        CHECK (ledger_checksum ~ '^sha256:[0-9a-f]{64}$'
            AND target_schema_bundle_digest ~ '^sha256:[0-9a-f]{64}$'
            AND restore_point_digest ~ '^sha256:[0-9a-f]{64}$'
            AND evidence_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT schema_restore_evidence_phase
        CHECK (cloud_agents.is_valid_identifier(target_phase)),
    CONSTRAINT schema_restore_evidence_time
        CHECK (drill_at <= created_at AND updated_at >= created_at)
);

CREATE TABLE cloud_agents.live_instances (
    service_kind text NOT NULL,
    instance_id text NOT NULL,
    incarnation bigint NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    rollout_generation bigint NOT NULL,
    writer_epoch bigint NOT NULL,
    binary_version text NOT NULL,
    supported_schema_min text NOT NULL,
    supported_schema_max text NOT NULL,
    drain_state text NOT NULL,
    heartbeat_at timestamptz NOT NULL,
    heartbeat_ttl_seconds integer NOT NULL,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (service_kind, instance_id, incarnation),
    CONSTRAINT live_instances_identity
        CHECK (cloud_agents.is_valid_identifier(service_kind)
            AND cloud_agents.is_valid_identifier(instance_id)
            AND incarnation >= 1
            AND rollout_generation >= 1
            AND writer_epoch >= 1),
    CONSTRAINT live_instances_registry
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest()
            AND profile_id = 'live-instance/v1'
            AND cloud_agents.compatibility_recovery_profile_is_registered(
                profile_id, profile_digest)),
    CONSTRAINT live_instances_binary
        CHECK (pg_catalog.octet_length(binary_version) BETWEEN 1 AND 128),
    CONSTRAINT live_instances_schema_range
        CHECK (supported_schema_min ~ '^[0-9]{6}$'
            AND supported_schema_max ~ '^[0-9]{6}$'
            AND supported_schema_min <= supported_schema_max),
    CONSTRAINT live_instances_drain_state
        CHECK (drain_state IN (
            'active', 'drained', 'draining', 'expired_unproven', 'registered', 'retired')),
    CONSTRAINT live_instances_heartbeat
        CHECK (heartbeat_ttl_seconds >= 1
            AND heartbeat_at >= created_at
            AND updated_at >= created_at)
);

CREATE TABLE cloud_agents.instance_retirement_receipts (
    service_kind text NOT NULL,
    instance_id text NOT NULL,
    incarnation bigint NOT NULL,
    rollout_generation bigint NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    state text NOT NULL,
    credential_revoked boolean NOT NULL DEFAULT false,
    endpoint_revoked boolean NOT NULL DEFAULT false,
    process_terminated boolean NOT NULL DEFAULT false,
    leader_released boolean NOT NULL DEFAULT false,
    claim_released boolean NOT NULL DEFAULT false,
    generation_fenced boolean NOT NULL DEFAULT false,
    writer_epoch bigint NOT NULL,
    receipt_digest text,
    rejection_reason text,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (service_kind, instance_id, incarnation, rollout_generation),
    CONSTRAINT instance_retirement_receipts_instance
        FOREIGN KEY (service_kind, instance_id, incarnation)
        REFERENCES cloud_agents.live_instances (service_kind, instance_id, incarnation),
    CONSTRAINT instance_retirement_receipts_identity
        CHECK (cloud_agents.is_valid_identifier(service_kind)
            AND cloud_agents.is_valid_identifier(instance_id)
            AND incarnation >= 1
            AND rollout_generation >= 1
            AND writer_epoch >= 1),
    CONSTRAINT instance_retirement_receipts_registry
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest()
            AND profile_id = 'retirement-receipt/v1'
            AND cloud_agents.compatibility_recovery_profile_is_registered(
                profile_id, profile_digest)),
    CONSTRAINT instance_retirement_receipts_state
        CHECK (state IN ('collecting', 'complete', 'rejected')),
    CONSTRAINT instance_retirement_receipts_complete
        CHECK (state <> 'complete' OR (
            credential_revoked
            AND endpoint_revoked
            AND process_terminated
            AND leader_released
            AND claim_released
            AND generation_fenced
            AND receipt_digest IS NOT NULL)),
    CONSTRAINT instance_retirement_receipts_digest
        CHECK (receipt_digest IS NULL OR receipt_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT instance_retirement_receipts_rejection
        CHECK (rejection_reason IS NULL
            OR cloud_agents.is_valid_identifier(rejection_reason)),
    CONSTRAINT instance_retirement_receipts_time
        CHECK (updated_at >= created_at)
);

CREATE INDEX workload_database_principals_instance_idx
    ON cloud_agents.workload_database_principals
    (service_kind, instance_id, incarnation, rollout_generation);
CREATE INDEX workload_database_principals_expiry_idx
    ON cloud_agents.workload_database_principals (state, expires_at);
CREATE INDEX migration_backfills_state_idx
    ON cloud_agents.migration_backfills (state, updated_at);
CREATE INDEX schema_restore_evidence_target_idx
    ON cloud_agents.schema_restore_evidence
    (target_schema_bundle_digest, target_phase, state);
CREATE INDEX live_instances_schema_range_idx
    ON cloud_agents.live_instances
    (supported_schema_min, supported_schema_max, drain_state);
CREATE INDEX live_instances_heartbeat_idx
    ON cloud_agents.live_instances (drain_state, heartbeat_at);
CREATE INDEX instance_retirement_receipts_state_idx
    ON cloud_agents.instance_retirement_receipts (state, updated_at);

ALTER FUNCTION cloud_agents.compatibility_recovery_registry_digest()
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.compatibility_recovery_state_machine_digest()
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.compatibility_recovery_policy_digest()
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.compatibility_recovery_profile_digest(text)
    OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.compatibility_recovery_profile_is_registered(text, text)
    OWNER TO cloud_agents_migration_owner;

ALTER TABLE cloud_agents.workload_database_principals
    OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.migration_backfills
    OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.schema_restore_evidence
    OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.live_instances
    OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents.instance_retirement_receipts
    OWNER TO cloud_agents_migration_owner;

REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_registry_digest()
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_state_machine_digest()
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_policy_digest()
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_profile_digest(text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_profile_is_registered(text, text)
    FROM PUBLIC;

REVOKE ALL ON TABLE cloud_agents.workload_database_principals FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.workload_database_principals FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.workload_database_principals FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.migration_backfills FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.migration_backfills FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.migration_backfills FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.schema_restore_evidence FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.schema_restore_evidence FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.schema_restore_evidence FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.live_instances FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.live_instances FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.live_instances FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.instance_retirement_receipts FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.instance_retirement_receipts FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.instance_retirement_receipts FROM cloud_agents_bootstrap_admin;

GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_registry_digest()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_state_machine_digest()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_policy_digest()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_profile_digest(text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_profile_is_registered(text, text)
    TO cloud_agents_runtime;
