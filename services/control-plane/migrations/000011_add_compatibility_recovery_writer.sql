-- P1-A2.4 slice B: append-only v2 compatibility/recovery writer kernel.
-- Historical v1 helpers and tables from 000010 remain byte-identical and are
-- not used as v2 authority. No function in this migration performs an external
-- call or exposes an HTTP, P2, provider, worker, session, turn, or execution
-- surface.

CREATE FUNCTION cloud_agents.compatibility_recovery_registry_digest_v2()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:d5ca128f28d637349dd6f8515f9ca6bb182fb0778a3160e24d731712589f2973'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_collect_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint, p_writer_epoch bigint,
    p_expected_version bigint, p_credential_revoked boolean,
    p_endpoint_revoked boolean, p_process_terminated boolean,
    p_leader_released boolean, p_claim_released boolean,
    p_generation_fenced boolean, p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    credential_revoked boolean, endpoint_revoked boolean,
    process_terminated boolean, leader_released boolean,
    claim_released boolean, generation_fenced boolean, receipt_digest text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    RETURN QUERY
    SELECT * FROM cloud_agents.compatibility_recovery_retirement_receipt_transition_v2(
        'collect', p_tenant_id, p_service_kind, p_instance_id,
        p_incarnation, p_rollout_generation, p_writer_epoch, p_expected_version,
        p_credential_revoked, p_endpoint_revoked, p_process_terminated,
        p_leader_released, p_claim_released, p_generation_fenced,
        NULL, NULL, p_transition_digest, p_request_digest
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_complete_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint, p_writer_epoch bigint,
    p_expected_version bigint, p_receipt_digest text,
    p_transition_digest text, p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    credential_revoked boolean, endpoint_revoked boolean,
    process_terminated boolean, leader_released boolean,
    claim_released boolean, generation_fenced boolean, receipt_digest text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    RETURN QUERY
    SELECT * FROM cloud_agents.compatibility_recovery_retirement_receipt_transition_v2(
        'complete', p_tenant_id, p_service_kind, p_instance_id,
        p_incarnation, p_rollout_generation, p_writer_epoch, p_expected_version,
        NULL, NULL, NULL, NULL, NULL, NULL, p_receipt_digest, NULL,
        p_transition_digest, p_request_digest
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_reject_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint, p_writer_epoch bigint,
    p_expected_version bigint, p_rejection_reason text,
    p_transition_digest text, p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    credential_revoked boolean, endpoint_revoked boolean,
    process_terminated boolean, leader_released boolean,
    claim_released boolean, generation_fenced boolean, receipt_digest text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    RETURN QUERY
    SELECT * FROM cloud_agents.compatibility_recovery_retirement_receipt_transition_v2(
        'reject', p_tenant_id, p_service_kind, p_instance_id,
        p_incarnation, p_rollout_generation, p_writer_epoch, p_expected_version,
        NULL, NULL, NULL, NULL, NULL, NULL, NULL, p_rejection_reason,
        p_transition_digest, p_request_digest
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_reconcile_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint,
    p_transition_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    credential_revoked boolean, endpoint_revoked boolean,
    process_terminated boolean, leader_released boolean,
    claim_released boolean, generation_fenced boolean, receipt_digest text,
    transition_observed boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    observed record;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_runtime'
    );
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_service_kind)
        OR NOT cloud_agents.is_valid_identifier(p_instance_id)
        OR p_incarnation < 1
        OR p_rollout_generation < 1
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'retirement-receipt reconcile input is invalid';
    END IF;
    SELECT receipt.*
    INTO observed
    FROM cloud_agents.compatibility_recovery_retirement_receipts_v2 AS receipt
    WHERE receipt.tenant_id = p_tenant_id
        AND receipt.service_kind = p_service_kind
        AND receipt.instance_id = p_instance_id
        AND receipt.incarnation = p_incarnation
        AND receipt.rollout_generation = p_rollout_generation;
    IF FOUND THEN
        result_code := 'observed';
        state := observed.state;
        version := observed.version;
        writer_epoch := observed.writer_epoch;
        credential_revoked := observed.credential_revoked;
        endpoint_revoked := observed.endpoint_revoked;
        process_terminated := observed.process_terminated;
        leader_released := observed.leader_released;
        claim_released := observed.claim_released;
        generation_fenced := observed.generation_fenced;
        receipt_digest := observed.receipt_digest;
    ELSE
        result_code := 'not_observed';
        state := 'absent';
        version := 0;
        writer_epoch := 0;
        credential_revoked := false;
        endpoint_revoked := false;
        process_terminated := false;
        leader_released := false;
        claim_released := false;
        generation_fenced := false;
        receipt_digest := NULL;
    END IF;
    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
        WHERE fact.tenant_id = p_tenant_id
            AND fact.transition_digest = p_transition_digest
            AND fact.profile_id = 'retirement-receipt/v2'
            AND fact.identity_digest =
                cloud_agents.compatibility_recovery_identity_digest_v2(
                    'retirement-receipt/v2', p_tenant_id, p_service_kind,
                    p_instance_id, p_incarnation::text,
                    p_rollout_generation::text
                )
    ) INTO transition_observed;
    write_applied := false;
    reconcile_required := false;
    database_timestamp := pg_catalog.transaction_timestamp();
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_state_machine_digest_v2()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:41ed340b8a1106341f8b797210492af0f9c022d8d43803977ff8079d52251863'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_policy_digest_v2()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:20f5b6e30e7d7254baabc97894aba2af2d2bcf40f4175f504d195b4e3a832708'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_schema_head_v2()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT '000010'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:a84a02c20244b60d2ffe4d27beb6fa5f5e0db8fb95ef91eef8865bce63412236'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_schema_migration_digest_v2()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:ab758a08c07ffb95b9e9a612c90079fcaf54d06407d0cfe4a0368db570f621e6'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_profile_digest_v2(
    p_profile_id text
)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT CASE p_profile_id
        WHEN 'backfill/v2'
            THEN 'sha256:c5d96407e0c0003689faa9e5526098b57e8b40d9ef67c76f9318e2b0326e6145'::text
        WHEN 'live-instance/v2'
            THEN 'sha256:0b2362b300f48a58160d5f9b754c865194f2a4ca14d6012fe361b340dd1b8ff8'::text
        WHEN 'migration-preflight/v2'
            THEN 'sha256:e02302ea60eca9855d362d8bcab7efc0466adab6d3a486d828adccdbc5411d7a'::text
        WHEN 'restore-evidence/v2'
            THEN 'sha256:c9a3376afb9e90717dc4191f88723488cf79bd5ee5df9ef24db1be8eb9a01106'::text
        WHEN 'retirement-receipt/v2'
            THEN 'sha256:b789a28be60a340f49662cd5c1570f29b30abd3b4b27ef76d7e6a2666833876f'::text
        WHEN 'workload-principal/v2'
            THEN 'sha256:7208b25e051ce6cb298d8f88190365a950bc0ac48a669fbf7ab93de35cee6878'::text
        ELSE NULL::text
    END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_profile_is_registered_v2(
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
        AND p_profile_digest = cloud_agents.compatibility_recovery_profile_digest_v2(p_profile_id),
        false
    )
$cloud_agents_function$;

CREATE TABLE cloud_agents.compatibility_recovery_workload_principals_v2 (
    tenant_id text NOT NULL,
    workload_id text NOT NULL,
    provider text NOT NULL,
    principal_id text NOT NULL,
    epoch bigint NOT NULL,
    state text NOT NULL,
    version bigint NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    schema_head text NOT NULL,
    schema_catalog_digest text NOT NULL,
    schema_migration_digest text NOT NULL,
    last_transition_digest text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    revoked_at timestamptz,
    PRIMARY KEY (tenant_id, workload_id, provider),
    UNIQUE (tenant_id, principal_id),
    CONSTRAINT compatibility_recovery_workload_principals_v2_identity
        CHECK (cloud_agents.is_valid_identifier(tenant_id)
            AND cloud_agents.is_valid_identifier(workload_id)
            AND cloud_agents.is_valid_identifier(provider)
            AND cloud_agents.is_valid_identifier(principal_id)),
    CONSTRAINT compatibility_recovery_workload_principals_v2_binding
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest_v2()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest_v2()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest_v2()
            AND profile_id = 'workload-principal/v2'
            AND cloud_agents.compatibility_recovery_profile_is_registered_v2(
                profile_id, profile_digest)
            AND schema_head = cloud_agents.compatibility_recovery_schema_head_v2()
            AND schema_catalog_digest = cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
            AND schema_migration_digest = cloud_agents.compatibility_recovery_schema_migration_digest_v2()),
    CONSTRAINT compatibility_recovery_workload_principals_v2_epoch
        CHECK (epoch >= 1 AND version >= 1),
    CONSTRAINT compatibility_recovery_workload_principals_v2_state
        CHECK (state IN ('active', 'revoked')
            AND ((state = 'active' AND revoked_at IS NULL)
                OR (state = 'revoked' AND revoked_at IS NOT NULL))),
    CONSTRAINT compatibility_recovery_workload_principals_v2_transition
        CHECK (last_transition_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT compatibility_recovery_workload_principals_v2_time
        CHECK (updated_at >= created_at
            AND (revoked_at IS NULL OR revoked_at = updated_at))
);

CREATE TABLE cloud_agents.compatibility_recovery_backfills_v2 (
    tenant_id text NOT NULL,
    migration_id text NOT NULL,
    state text NOT NULL,
    phase text NOT NULL,
    cursor text NOT NULL,
    digest text NOT NULL,
    count bigint NOT NULL,
    writer_epoch bigint NOT NULL,
    lease_owner text,
    lease_expires_at timestamptz,
    version bigint NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    schema_head text NOT NULL,
    schema_catalog_digest text NOT NULL,
    schema_migration_digest text NOT NULL,
    last_transition_digest text NOT NULL,
    stable_error_code text,
    committed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (tenant_id, migration_id),
    CONSTRAINT compatibility_recovery_backfills_v2_identity
        CHECK (cloud_agents.is_valid_identifier(tenant_id)
            AND migration_id ~ '^[0-9]{6}$'),
    CONSTRAINT compatibility_recovery_backfills_v2_binding
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest_v2()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest_v2()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest_v2()
            AND profile_id = 'backfill/v2'
            AND cloud_agents.compatibility_recovery_profile_is_registered_v2(
                profile_id, profile_digest)
            AND schema_head = cloud_agents.compatibility_recovery_schema_head_v2()
            AND schema_catalog_digest = cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
            AND schema_migration_digest = cloud_agents.compatibility_recovery_schema_migration_digest_v2()),
    CONSTRAINT compatibility_recovery_backfills_v2_state
        CHECK (state IN ('failed', 'leased', 'paused', 'pending', 'running', 'succeeded')),
    CONSTRAINT compatibility_recovery_backfills_v2_payload
        CHECK (cloud_agents.is_valid_identifier(phase)
            AND pg_catalog.octet_length(cursor) BETWEEN 1 AND 2048
            AND digest ~ '^sha256:[0-9a-f]{64}$'
            AND count >= 0
            AND writer_epoch >= 1
            AND version >= 1),
    CONSTRAINT compatibility_recovery_backfills_v2_lease
        CHECK ((state IN ('leased', 'running')
                AND cloud_agents.is_valid_identifier(lease_owner)
                AND lease_expires_at IS NOT NULL)
            OR (state NOT IN ('leased', 'running')
                AND lease_owner IS NULL
                AND lease_expires_at IS NULL)),
    CONSTRAINT compatibility_recovery_backfills_v2_terminal
        CHECK ((state = 'succeeded' AND committed_at IS NOT NULL)
            OR (state <> 'succeeded' AND committed_at IS NULL)),
    CONSTRAINT compatibility_recovery_backfills_v2_error
        CHECK (stable_error_code IS NULL
            OR cloud_agents.is_valid_identifier(stable_error_code)),
    CONSTRAINT compatibility_recovery_backfills_v2_transition
        CHECK (last_transition_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT compatibility_recovery_backfills_v2_time
        CHECK (updated_at >= created_at
            AND (committed_at IS NULL OR committed_at = updated_at))
);

CREATE TABLE cloud_agents.compatibility_recovery_restore_evidence_v2 (
    tenant_id text NOT NULL,
    drill_id text NOT NULL,
    state text NOT NULL,
    scope text NOT NULL,
    postgres_major integer NOT NULL,
    ledger_checksum text NOT NULL,
    target_schema_bundle_digest text NOT NULL,
    target_phase text NOT NULL,
    restore_point_digest text NOT NULL,
    evidence_digest text NOT NULL,
    drill_at timestamptz NOT NULL,
    version bigint NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    schema_head text NOT NULL,
    schema_catalog_digest text NOT NULL,
    schema_migration_digest text NOT NULL,
    last_transition_digest text NOT NULL,
    rejection_reason text,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (tenant_id, drill_id),
    UNIQUE (tenant_id, evidence_digest),
    CONSTRAINT compatibility_recovery_restore_evidence_v2_identity
        CHECK (cloud_agents.is_valid_identifier(tenant_id)
            AND cloud_agents.is_valid_identifier(drill_id)),
    CONSTRAINT compatibility_recovery_restore_evidence_v2_binding
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest_v2()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest_v2()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest_v2()
            AND profile_id = 'restore-evidence/v2'
            AND cloud_agents.compatibility_recovery_profile_is_registered_v2(
                profile_id, profile_digest)
            AND schema_head = cloud_agents.compatibility_recovery_schema_head_v2()
            AND schema_catalog_digest = cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
            AND schema_migration_digest = cloud_agents.compatibility_recovery_schema_migration_digest_v2()),
    CONSTRAINT compatibility_recovery_restore_evidence_v2_state
        CHECK (state IN ('complete', 'recorded', 'rejected')
            AND ((state = 'rejected' AND cloud_agents.is_valid_identifier(rejection_reason))
                OR (state <> 'rejected' AND rejection_reason IS NULL))),
    CONSTRAINT compatibility_recovery_restore_evidence_v2_scope
        CHECK (scope = 'local_logical_backup_restore_and_preflight'),
    CONSTRAINT compatibility_recovery_restore_evidence_v2_payload
        CHECK (postgres_major BETWEEN 15 AND 17
            AND ledger_checksum ~ '^sha256:[0-9a-f]{64}$'
            AND target_schema_bundle_digest ~ '^sha256:[0-9a-f]{64}$'
            AND target_phase ~ '^[0-9]{6}$'
            AND restore_point_digest ~ '^sha256:[0-9a-f]{64}$'
            AND evidence_digest ~ '^sha256:[0-9a-f]{64}$'
            AND version >= 1),
    CONSTRAINT compatibility_recovery_restore_evidence_v2_transition
        CHECK (last_transition_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT compatibility_recovery_restore_evidence_v2_time
        CHECK (drill_at <= created_at AND updated_at >= created_at)
);

CREATE TABLE cloud_agents.compatibility_recovery_live_instances_v2 (
    tenant_id text NOT NULL,
    service_kind text NOT NULL,
    instance_id text NOT NULL,
    incarnation bigint NOT NULL,
    rollout_generation bigint NOT NULL,
    writer_epoch bigint NOT NULL,
    binary_version text NOT NULL,
    supported_schema_min text NOT NULL,
    supported_schema_max text NOT NULL,
    drain_state text NOT NULL,
    heartbeat_at timestamptz NOT NULL,
    heartbeat_ttl_seconds integer NOT NULL,
    version bigint NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    schema_head text NOT NULL,
    schema_catalog_digest text NOT NULL,
    schema_migration_digest text NOT NULL,
    last_transition_digest text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (tenant_id, service_kind, instance_id, incarnation),
    UNIQUE (tenant_id, service_kind, instance_id, incarnation, rollout_generation),
    CONSTRAINT compatibility_recovery_live_instances_v2_identity
        CHECK (cloud_agents.is_valid_identifier(tenant_id)
            AND cloud_agents.is_valid_identifier(service_kind)
            AND cloud_agents.is_valid_identifier(instance_id)
            AND incarnation >= 1
            AND rollout_generation >= 1
            AND writer_epoch >= 1
            AND version >= 1),
    CONSTRAINT compatibility_recovery_live_instances_v2_binding
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest_v2()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest_v2()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest_v2()
            AND profile_id = 'live-instance/v2'
            AND cloud_agents.compatibility_recovery_profile_is_registered_v2(
                profile_id, profile_digest)
            AND schema_head = cloud_agents.compatibility_recovery_schema_head_v2()
            AND schema_catalog_digest = cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
            AND schema_migration_digest = cloud_agents.compatibility_recovery_schema_migration_digest_v2()),
    CONSTRAINT compatibility_recovery_live_instances_v2_binary
        CHECK (pg_catalog.octet_length(binary_version) BETWEEN 1 AND 128),
    CONSTRAINT compatibility_recovery_live_instances_v2_schema_range
        CHECK (supported_schema_min ~ '^[0-9]{6}$'
            AND supported_schema_max ~ '^[0-9]{6}$'
            AND supported_schema_min <= supported_schema_max),
    CONSTRAINT compatibility_recovery_live_instances_v2_state
        CHECK (drain_state IN ('active', 'drained', 'draining', 'fenced', 'registered')),
    CONSTRAINT compatibility_recovery_live_instances_v2_heartbeat
        CHECK (heartbeat_ttl_seconds BETWEEN 1 AND 300
            AND heartbeat_at >= created_at
            AND updated_at >= created_at),
    CONSTRAINT compatibility_recovery_live_instances_v2_transition
        CHECK (last_transition_digest ~ '^sha256:[0-9a-f]{64}$')
);

CREATE TABLE cloud_agents.compatibility_recovery_retirement_receipts_v2 (
    tenant_id text NOT NULL,
    service_kind text NOT NULL,
    instance_id text NOT NULL,
    incarnation bigint NOT NULL,
    rollout_generation bigint NOT NULL,
    writer_epoch bigint NOT NULL,
    state text NOT NULL,
    credential_revoked boolean NOT NULL,
    endpoint_revoked boolean NOT NULL,
    process_terminated boolean NOT NULL,
    leader_released boolean NOT NULL,
    claim_released boolean NOT NULL,
    generation_fenced boolean NOT NULL,
    receipt_digest text,
    rejection_reason text,
    version bigint NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    schema_head text NOT NULL,
    schema_catalog_digest text NOT NULL,
    schema_migration_digest text NOT NULL,
    last_transition_digest text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (tenant_id, service_kind, instance_id, incarnation, rollout_generation),
    CONSTRAINT compatibility_recovery_retirement_receipts_v2_instance
        FOREIGN KEY (tenant_id, service_kind, instance_id, incarnation, rollout_generation)
        REFERENCES cloud_agents.compatibility_recovery_live_instances_v2
            (tenant_id, service_kind, instance_id, incarnation, rollout_generation),
    CONSTRAINT compatibility_recovery_retirement_receipts_v2_identity
        CHECK (cloud_agents.is_valid_identifier(tenant_id)
            AND cloud_agents.is_valid_identifier(service_kind)
            AND cloud_agents.is_valid_identifier(instance_id)
            AND incarnation >= 1
            AND rollout_generation >= 1
            AND writer_epoch >= 1
            AND version >= 1),
    CONSTRAINT compatibility_recovery_retirement_receipts_v2_binding
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest_v2()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest_v2()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest_v2()
            AND profile_id = 'retirement-receipt/v2'
            AND cloud_agents.compatibility_recovery_profile_is_registered_v2(
                profile_id, profile_digest)
            AND schema_head = cloud_agents.compatibility_recovery_schema_head_v2()
            AND schema_catalog_digest = cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
            AND schema_migration_digest = cloud_agents.compatibility_recovery_schema_migration_digest_v2()),
    CONSTRAINT compatibility_recovery_retirement_receipts_v2_state
        CHECK (state IN ('collecting', 'complete', 'rejected')),
    CONSTRAINT compatibility_recovery_retirement_receipts_v2_complete
        CHECK (state <> 'complete' OR (
            credential_revoked
            AND endpoint_revoked
            AND process_terminated
            AND leader_released
            AND claim_released
            AND generation_fenced
            AND receipt_digest IS NOT NULL)),
    CONSTRAINT compatibility_recovery_retirement_receipts_v2_rejection
        CHECK ((state = 'rejected' AND cloud_agents.is_valid_identifier(rejection_reason))
            OR (state <> 'rejected' AND rejection_reason IS NULL)),
    CONSTRAINT compatibility_recovery_retirement_receipts_v2_digest
        CHECK ((receipt_digest IS NULL OR receipt_digest ~ '^sha256:[0-9a-f]{64}$')
            AND last_transition_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT compatibility_recovery_retirement_receipts_v2_time
        CHECK (updated_at >= created_at)
);

CREATE TABLE cloud_agents.compatibility_recovery_transition_facts_v2 (
    tenant_id text NOT NULL,
    operation_id text NOT NULL,
    transition_digest text NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    schema_head text NOT NULL,
    schema_catalog_digest text NOT NULL,
    schema_migration_digest text NOT NULL,
    identity_digest text NOT NULL,
    request_digest text NOT NULL,
    actor_principal text NOT NULL,
    result_code text NOT NULL,
    state text NOT NULL,
    version bigint NOT NULL,
    writer_epoch bigint NOT NULL,
    stable_error_code text,
    database_timestamp timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, transition_digest),
    CONSTRAINT compatibility_recovery_transition_facts_v2_identity
        CHECK (cloud_agents.is_valid_identifier(tenant_id)
            AND cloud_agents.is_valid_identifier(actor_principal)
            AND identity_digest ~ '^sha256:[0-9a-f]{64}$'
            AND request_digest ~ '^sha256:[0-9a-f]{64}$'
            AND transition_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT compatibility_recovery_transition_facts_v2_binding
        CHECK (registry_digest = cloud_agents.compatibility_recovery_registry_digest_v2()
            AND state_machine_digest = cloud_agents.compatibility_recovery_state_machine_digest_v2()
            AND policy_digest = cloud_agents.compatibility_recovery_policy_digest_v2()
            AND cloud_agents.compatibility_recovery_profile_is_registered_v2(
                profile_id, profile_digest)
            AND schema_head = cloud_agents.compatibility_recovery_schema_head_v2()
            AND schema_catalog_digest = cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
            AND schema_migration_digest = cloud_agents.compatibility_recovery_schema_migration_digest_v2()),
    CONSTRAINT compatibility_recovery_transition_facts_v2_operation
        CHECK (operation_id IN (
            'backfill-acquire-lease/v2',
            'backfill-advance/v2',
            'backfill-complete/v2',
            'backfill-heartbeat/v2',
            'backfill-start/v2',
            'live-instance-activate/v2',
            'live-instance-begin-drain/v2',
            'live-instance-fence/v2',
            'live-instance-finish-drain/v2',
            'live-instance-heartbeat/v2',
            'live-instance-register/v2',
            'restore-evidence-complete/v2',
            'restore-evidence-record/v2',
            'restore-evidence-reject/v2',
            'retirement-receipt-collect/v2',
            'retirement-receipt-complete/v2',
            'retirement-receipt-reject/v2',
            'workload-principal-register/v2',
            'workload-principal-revoke/v2',
            'workload-principal-rotate/v2'
        )),
    CONSTRAINT compatibility_recovery_transition_facts_v2_result
        CHECK (result_code = 'applied'
            AND pg_catalog.octet_length(state) BETWEEN 1 AND 64
            AND version >= 1
            AND writer_epoch >= 0
            AND (stable_error_code IS NULL
                OR cloud_agents.is_valid_identifier(stable_error_code)))
);

CREATE FUNCTION cloud_agents.compatibility_recovery_identity_digest_v2(
    p_domain text,
    p_tenant_id text,
    p_part_1 text,
    p_part_2 text,
    p_part_3 text,
    p_part_4 text
)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:' || pg_catalog.encode(
        pg_catalog.sha256(
            pg_catalog.convert_to(
                pg_catalog.jsonb_build_array(
                    p_domain, p_tenant_id, p_part_1, p_part_2, p_part_3, p_part_4
                )::text,
                'UTF8'
            )
        ),
        'hex'
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_lock_v2(
    p_tenant_id text,
    p_profile_id text,
    p_identity_digest text
)
RETURNS void
LANGUAGE sql
VOLATILE
PARALLEL UNSAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            pg_catalog.jsonb_build_array(
                'cloud-agents-compatibility-recovery-lock/v2',
                p_tenant_id,
                p_profile_id,
                p_identity_digest
            )::text,
            0
        )
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_transition_lock_v2(
    p_tenant_id text,
    p_transition_digest text
)
RETURNS void
LANGUAGE sql
VOLATILE
PARALLEL UNSAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended(
            pg_catalog.jsonb_build_array(
                'cloud-agents-compatibility-recovery-transition-lock/v2',
                p_tenant_id,
                p_transition_digest
            )::text,
            0
        )
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_require_principal_v2(
    p_expected_group text
)
RETURNS text
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    expected_role record;
    caller_role record;
    caller_membership record;
BEGIN
    IF p_expected_group NOT IN (
        'cloud_agents_migration_owner',
        'cloud_agents_runtime',
        'cloud_agents_bootstrap_admin'
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'compatibility/recovery authority group is invalid';
    END IF;

    SELECT
        role_row.oid,
        role_row.rolcanlogin,
        role_row.rolinherit,
        role_row.rolsuper,
        role_row.rolcreatedb,
        role_row.rolcreaterole,
        role_row.rolreplication,
        role_row.rolbypassrls
    INTO STRICT expected_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = p_expected_group;

    IF expected_role.rolcanlogin
        OR expected_role.rolinherit
        OR expected_role.rolsuper
        OR expected_role.rolcreatedb
        OR expected_role.rolcreaterole
        OR expected_role.rolreplication
        OR expected_role.rolbypassrls
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = expected_role.oid
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'compatibility/recovery authority group drift';
    END IF;

    SELECT
        role_row.oid,
        role_row.rolcanlogin,
        role_row.rolinherit,
        role_row.rolsuper,
        role_row.rolcreatedb,
        role_row.rolcreaterole,
        role_row.rolreplication,
        role_row.rolbypassrls
    INTO STRICT caller_role
    FROM pg_catalog.pg_roles AS role_row
    WHERE role_row.rolname = SESSION_USER;

    IF SESSION_USER IN (
        'cloud_agents_migration_owner',
        'cloud_agents_runtime',
        'cloud_agents_bootstrap_admin'
    )
        OR NOT caller_role.rolcanlogin
        OR caller_role.rolsuper
        OR caller_role.rolcreatedb
        OR caller_role.rolcreaterole
        OR caller_role.rolreplication
        OR caller_role.rolbypassrls
        OR EXISTS (
            SELECT 1
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.roleid = caller_role.oid
        )
        OR (
            SELECT pg_catalog.count(*)
            FROM pg_catalog.pg_auth_members AS membership
            WHERE membership.member = caller_role.oid
        ) <> 1
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'session user is outside the compatibility/recovery authority boundary';
    END IF;

    SELECT
        membership.admin_option,
        coalesce(
            (pg_catalog.to_jsonb(membership)->>'inherit_option')::boolean,
            true
        ) AS membership_inherits,
        coalesce(
            (pg_catalog.to_jsonb(membership)->>'set_option')::boolean,
            true
        ) AS membership_is_settable,
        grantor_role.oid AS grantor_oid,
        grantor_role.rolname AS grantor_name,
        grantor_role.rolsuper AS grantor_is_superuser
    INTO caller_membership
    FROM pg_catalog.pg_auth_members AS membership
    JOIN pg_catalog.pg_roles AS grantor_role
        ON grantor_role.oid = membership.grantor
    WHERE membership.roleid = expected_role.oid
        AND membership.member = caller_role.oid;

    IF NOT FOUND
        OR caller_membership.admin_option
        OR NOT caller_membership.membership_is_settable
        OR NOT caller_membership.grantor_is_superuser
        OR caller_membership.grantor_name IN (
            'cloud_agents_migration_owner',
            'cloud_agents_runtime',
            'cloud_agents_bootstrap_admin'
        )
        OR EXISTS (
            WITH RECURSIVE grantor_memberships (roleid) AS (
                SELECT membership.roleid
                FROM pg_catalog.pg_auth_members AS membership
                WHERE membership.member = caller_membership.grantor_oid

                UNION

                SELECT membership.roleid
                FROM pg_catalog.pg_auth_members AS membership
                JOIN grantor_memberships
                    ON grantor_memberships.roleid = membership.member
            )
            SELECT 1
            FROM grantor_memberships
            JOIN pg_catalog.pg_roles AS inherited_role
                ON inherited_role.oid = grantor_memberships.roleid
            WHERE inherited_role.rolname IN (
                'cloud_agents_migration_owner',
                'cloud_agents_runtime',
                'cloud_agents_bootstrap_admin'
            )
        )
        OR (
            p_expected_group = 'cloud_agents_migration_owner'
            AND (
                caller_role.rolinherit
                OR pg_catalog.pg_has_role(SESSION_USER, p_expected_group, 'USAGE')
            )
        )
        OR (
            p_expected_group <> 'cloud_agents_migration_owner'
            AND (
                NOT caller_role.rolinherit
                OR NOT caller_membership.membership_inherits
                OR NOT pg_catalog.pg_has_role(SESSION_USER, p_expected_group, 'USAGE')
            )
        )
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '42501',
            MESSAGE = 'session user compatibility/recovery membership drift';
    END IF;

    RETURN SESSION_USER;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_restore_evidence_transition_v2(
    p_action text,
    p_tenant_id text,
    p_drill_id text,
    p_expected_version bigint,
    p_postgres_major integer,
    p_ledger_checksum text,
    p_target_schema_bundle_digest text,
    p_target_phase text,
    p_restore_point_digest text,
    p_evidence_digest text,
    p_drill_at timestamptz,
    p_rejection_reason text,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text,
    evidence_digest text,
    target_schema_bundle_digest text,
    target_phase text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    expected_profile_id constant text := 'restore-evidence/v2';
    expected_profile_digest constant text := 'sha256:c9a3376afb9e90717dc4191f88723488cf79bd5ee5df9ef24db1be8eb9a01106';
    operation_id text;
    actor_principal text;
    identity_digest text;
    transitioned_at timestamptz;
    stored record;
    stored_fact record;
    had_row boolean;
    inserted_count bigint;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_migration_owner'
    );
    transitioned_at := pg_catalog.transaction_timestamp();
    operation_id := CASE p_action
        WHEN 'record' THEN 'restore-evidence-record/v2'
        WHEN 'complete' THEN 'restore-evidence-complete/v2'
        WHEN 'reject' THEN 'restore-evidence-reject/v2'
        ELSE NULL
    END;
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_drill_id)
        OR operation_id IS NULL
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.compatibility_recovery_profile_is_registered_v2(
            expected_profile_id, expected_profile_digest)
        OR (p_action = 'record' AND (
            p_expected_version IS NOT NULL
            OR p_postgres_major NOT BETWEEN 15 AND 17
            OR p_ledger_checksum !~ '^sha256:[0-9a-f]{64}$'
            OR p_target_schema_bundle_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_target_phase !~ '^[0-9]{6}$'
            OR p_restore_point_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_evidence_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_drill_at IS NULL
            OR p_drill_at > transitioned_at
            OR p_rejection_reason IS NOT NULL))
        OR (p_action = 'complete' AND (
            p_expected_version < 1
            OR p_postgres_major IS NOT NULL
            OR p_ledger_checksum IS NOT NULL
            OR p_target_schema_bundle_digest IS NOT NULL
            OR p_target_phase IS NOT NULL
            OR p_restore_point_digest IS NOT NULL
            OR p_evidence_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_drill_at IS NOT NULL
            OR p_rejection_reason IS NOT NULL))
        OR (p_action = 'reject' AND (
            p_expected_version < 1
            OR p_postgres_major IS NOT NULL
            OR p_ledger_checksum IS NOT NULL
            OR p_target_schema_bundle_digest IS NOT NULL
            OR p_target_phase IS NOT NULL
            OR p_restore_point_digest IS NOT NULL
            OR p_evidence_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_drill_at IS NOT NULL
            OR NOT cloud_agents.is_valid_identifier(p_rejection_reason)))
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'restore-evidence transition input is invalid';
    END IF;

    PERFORM cloud_agents.compatibility_recovery_transition_lock_v2(
        p_tenant_id, p_transition_digest
    );
    identity_digest := cloud_agents.compatibility_recovery_identity_digest_v2(
        'restore-evidence/v2', p_tenant_id, p_drill_id, NULL, NULL, NULL
    );
    PERFORM cloud_agents.compatibility_recovery_lock_v2(
        p_tenant_id, expected_profile_id, identity_digest
    );
    SELECT fact.*
    INTO stored_fact
    FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
    WHERE fact.tenant_id = p_tenant_id
        AND fact.transition_digest = p_transition_digest
    FOR UPDATE;
    IF FOUND THEN
        IF stored_fact.operation_id = operation_id
            AND stored_fact.profile_id = expected_profile_id
            AND stored_fact.profile_digest = expected_profile_digest
            AND stored_fact.identity_digest = identity_digest
            AND stored_fact.request_digest = p_request_digest
            AND stored_fact.actor_principal = actor_principal
        THEN
            result_code := 'observed';
            state := stored_fact.state;
            version := stored_fact.version;
            writer_epoch := 0;
            database_timestamp := stored_fact.database_timestamp;
            stable_error_code := stored_fact.stable_error_code;
            SELECT evidence.evidence_digest,
                evidence.target_schema_bundle_digest,
                evidence.target_phase
            INTO evidence_digest, target_schema_bundle_digest, target_phase
            FROM cloud_agents.compatibility_recovery_restore_evidence_v2 AS evidence
            WHERE evidence.tenant_id = p_tenant_id
                AND evidence.drill_id = p_drill_id;
        ELSE
            result_code := 'conflict';
            state := 'unknown';
            version := 0;
            writer_epoch := 0;
            database_timestamp := transitioned_at;
            stable_error_code := 'transition_digest_conflict';
            evidence_digest := NULL;
            target_schema_bundle_digest := NULL;
            target_phase := NULL;
        END IF;
        write_applied := false;
        reconcile_required := false;
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT evidence.*
    INTO stored
    FROM cloud_agents.compatibility_recovery_restore_evidence_v2 AS evidence
    WHERE evidence.tenant_id = p_tenant_id
        AND evidence.drill_id = p_drill_id
    FOR UPDATE;
    had_row := FOUND;

    IF p_action = 'record' THEN
        IF had_row THEN
            result_code := 'rejected';
            stable_error_code := 'restore_evidence_already_recorded';
        ELSIF EXISTS (
            SELECT 1
            FROM cloud_agents.compatibility_recovery_restore_evidence_v2 AS evidence
            WHERE evidence.tenant_id = p_tenant_id
                AND evidence.evidence_digest = p_evidence_digest
        ) THEN
            result_code := 'rejected';
            stable_error_code := 'restore_evidence_digest_conflict';
        ELSE
            INSERT INTO cloud_agents.compatibility_recovery_restore_evidence_v2 (
                tenant_id, drill_id, state, scope, postgres_major,
                ledger_checksum, target_schema_bundle_digest, target_phase,
                restore_point_digest, evidence_digest, drill_at, version,
                registry_digest, state_machine_digest, policy_digest,
                profile_id, profile_digest, schema_head, schema_catalog_digest,
                schema_migration_digest, last_transition_digest,
                rejection_reason, created_at, updated_at
            ) VALUES (
                p_tenant_id, p_drill_id, 'recorded',
                'local_logical_backup_restore_and_preflight', p_postgres_major,
                p_ledger_checksum, p_target_schema_bundle_digest, p_target_phase,
                p_restore_point_digest, p_evidence_digest, p_drill_at, 1,
                cloud_agents.compatibility_recovery_registry_digest_v2(),
                cloud_agents.compatibility_recovery_state_machine_digest_v2(),
                cloud_agents.compatibility_recovery_policy_digest_v2(),
                expected_profile_id, expected_profile_digest,
                cloud_agents.compatibility_recovery_schema_head_v2(),
                cloud_agents.compatibility_recovery_schema_catalog_digest_v2(),
                cloud_agents.compatibility_recovery_schema_migration_digest_v2(),
                p_transition_digest, NULL, transitioned_at, transitioned_at
            )
            ON CONFLICT DO NOTHING;
            GET DIAGNOSTICS inserted_count = ROW_COUNT;
            IF inserted_count = 0 THEN
                result_code := 'rejected';
                stable_error_code := 'restore_evidence_digest_conflict';
            ELSE
                state := 'recorded';
                version := 1;
                evidence_digest := p_evidence_digest;
                target_schema_bundle_digest := p_target_schema_bundle_digest;
                target_phase := p_target_phase;
            END IF;
        END IF;
    ELSIF NOT had_row THEN
        result_code := 'rejected';
        stable_error_code := 'restore_evidence_absent';
    ELSIF stored.version <> p_expected_version
        OR stored.evidence_digest <> p_evidence_digest
    THEN
        result_code := 'rejected';
        stable_error_code := 'restore_evidence_fence_stale';
    ELSIF stored.state <> 'recorded' THEN
        result_code := 'rejected';
        stable_error_code := 'restore_evidence_state_conflict';
    ELSE
        UPDATE cloud_agents.compatibility_recovery_restore_evidence_v2 AS evidence
        SET
            state = CASE p_action
                WHEN 'complete' THEN 'complete'
                ELSE 'rejected'
            END,
            rejection_reason = CASE
                WHEN p_action = 'reject' THEN p_rejection_reason
                ELSE NULL
            END,
            version = evidence.version + 1,
            last_transition_digest = p_transition_digest,
            updated_at = transitioned_at
        WHERE evidence.tenant_id = p_tenant_id
            AND evidence.drill_id = p_drill_id
        RETURNING evidence.state, evidence.version, evidence.evidence_digest,
            evidence.target_schema_bundle_digest, evidence.target_phase
        INTO state, version, evidence_digest,
            target_schema_bundle_digest, target_phase;
    END IF;

    writer_epoch := 0;
    IF result_code = 'rejected' THEN
        state := CASE WHEN had_row THEN stored.state ELSE 'absent' END;
        version := CASE WHEN had_row THEN stored.version ELSE 0 END;
        evidence_digest := CASE
            WHEN had_row THEN stored.evidence_digest ELSE NULL
        END;
        target_schema_bundle_digest := CASE
            WHEN had_row THEN stored.target_schema_bundle_digest ELSE NULL
        END;
        target_phase := CASE WHEN had_row THEN stored.target_phase ELSE NULL END;
        write_applied := false;
        reconcile_required := false;
        database_timestamp := transitioned_at;
        RETURN NEXT;
        RETURN;
    END IF;

    PERFORM cloud_agents.compatibility_recovery_record_transition_v2(
        p_tenant_id, operation_id, p_transition_digest,
        expected_profile_id, expected_profile_digest, identity_digest,
        p_request_digest, actor_principal, state, version, 0,
        NULL, transitioned_at
    );
    result_code := 'applied';
    write_applied := true;
    reconcile_required := false;
    database_timestamp := transitioned_at;
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_restore_evidence_record_v2(
    p_tenant_id text,
    p_drill_id text,
    p_postgres_major integer,
    p_ledger_checksum text,
    p_target_schema_bundle_digest text,
    p_target_phase text,
    p_restore_point_digest text,
    p_evidence_digest text,
    p_drill_at timestamptz,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    evidence_digest text, target_schema_bundle_digest text, target_phase text
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.compatibility_recovery_restore_evidence_transition_v2(
        'record', p_tenant_id, p_drill_id, NULL, p_postgres_major,
        p_ledger_checksum, p_target_schema_bundle_digest, p_target_phase,
        p_restore_point_digest, p_evidence_digest, p_drill_at, NULL,
        p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_restore_evidence_complete_v2(
    p_tenant_id text,
    p_drill_id text,
    p_expected_version bigint,
    p_evidence_digest text,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    evidence_digest text, target_schema_bundle_digest text, target_phase text
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.compatibility_recovery_restore_evidence_transition_v2(
        'complete', p_tenant_id, p_drill_id, p_expected_version,
        NULL, NULL, NULL, NULL, NULL, p_evidence_digest, NULL, NULL,
        p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_restore_evidence_reject_v2(
    p_tenant_id text,
    p_drill_id text,
    p_expected_version bigint,
    p_evidence_digest text,
    p_rejection_reason text,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    evidence_digest text, target_schema_bundle_digest text, target_phase text
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.compatibility_recovery_restore_evidence_transition_v2(
        'reject', p_tenant_id, p_drill_id, p_expected_version,
        NULL, NULL, NULL, NULL, NULL, p_evidence_digest, NULL,
        p_rejection_reason, p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_restore_evidence_reconcile_v2(
    p_tenant_id text,
    p_drill_id text,
    p_transition_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    evidence_digest text, target_schema_bundle_digest text, target_phase text,
    transition_observed boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    observed record;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_migration_owner'
    );
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_drill_id)
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'restore-evidence reconcile input is invalid';
    END IF;
    SELECT evidence.*
    INTO observed
    FROM cloud_agents.compatibility_recovery_restore_evidence_v2 AS evidence
    WHERE evidence.tenant_id = p_tenant_id
        AND evidence.drill_id = p_drill_id;
    IF FOUND THEN
        result_code := 'observed';
        state := observed.state;
        version := observed.version;
        evidence_digest := observed.evidence_digest;
        target_schema_bundle_digest := observed.target_schema_bundle_digest;
        target_phase := observed.target_phase;
    ELSE
        result_code := 'not_observed';
        state := 'absent';
        version := 0;
        evidence_digest := NULL;
        target_schema_bundle_digest := NULL;
        target_phase := NULL;
    END IF;
    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
        WHERE fact.tenant_id = p_tenant_id
            AND fact.transition_digest = p_transition_digest
            AND fact.profile_id = 'restore-evidence/v2'
            AND fact.identity_digest =
                cloud_agents.compatibility_recovery_identity_digest_v2(
                    'restore-evidence/v2', p_tenant_id, p_drill_id,
                    NULL, NULL, NULL
                )
    ) INTO transition_observed;
    write_applied := false;
    reconcile_required := false;
    writer_epoch := 0;
    database_timestamp := pg_catalog.transaction_timestamp();
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_transition_v2(
    p_action text,
    p_tenant_id text,
    p_service_kind text,
    p_instance_id text,
    p_incarnation bigint,
    p_rollout_generation bigint,
    p_writer_epoch bigint,
    p_expected_version bigint,
    p_credential_revoked boolean,
    p_endpoint_revoked boolean,
    p_process_terminated boolean,
    p_leader_released boolean,
    p_claim_released boolean,
    p_generation_fenced boolean,
    p_receipt_digest text,
    p_rejection_reason text,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text,
    credential_revoked boolean,
    endpoint_revoked boolean,
    process_terminated boolean,
    leader_released boolean,
    claim_released boolean,
    generation_fenced boolean,
    receipt_digest text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    expected_profile_id constant text := 'retirement-receipt/v2';
    expected_profile_digest constant text := 'sha256:b789a28be60a340f49662cd5c1570f29b30abd3b4b27ef76d7e6a2666833876f';
    operation_id text;
    actor_principal text;
    identity_digest text;
    transitioned_at timestamptz;
    stored record;
    live_instance record;
    stored_fact record;
    had_row boolean;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_runtime'
    );
    transitioned_at := pg_catalog.transaction_timestamp();
    operation_id := CASE p_action
        WHEN 'collect' THEN 'retirement-receipt-collect/v2'
        WHEN 'complete' THEN 'retirement-receipt-complete/v2'
        WHEN 'reject' THEN 'retirement-receipt-reject/v2'
        ELSE NULL
    END;
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_service_kind)
        OR NOT cloud_agents.is_valid_identifier(p_instance_id)
        OR p_incarnation < 1
        OR p_rollout_generation < 1
        OR p_writer_epoch < 1
        OR operation_id IS NULL
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.compatibility_recovery_profile_is_registered_v2(
            expected_profile_id, expected_profile_digest)
        OR (p_action = 'collect' AND (
            p_expected_version < 0
            OR p_credential_revoked IS NULL
            OR p_endpoint_revoked IS NULL
            OR p_process_terminated IS NULL
            OR p_leader_released IS NULL
            OR p_claim_released IS NULL
            OR p_generation_fenced IS NULL
            OR NOT (p_credential_revoked OR p_endpoint_revoked
                OR p_process_terminated OR p_leader_released
                OR p_claim_released OR p_generation_fenced)
            OR p_receipt_digest IS NOT NULL
            OR p_rejection_reason IS NOT NULL))
        OR (p_action = 'complete' AND (
            p_expected_version < 1
            OR p_credential_revoked IS NOT NULL
            OR p_endpoint_revoked IS NOT NULL
            OR p_process_terminated IS NOT NULL
            OR p_leader_released IS NOT NULL
            OR p_claim_released IS NOT NULL
            OR p_generation_fenced IS NOT NULL
            OR p_receipt_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_rejection_reason IS NOT NULL))
        OR (p_action = 'reject' AND (
            p_expected_version < 1
            OR p_credential_revoked IS NOT NULL
            OR p_endpoint_revoked IS NOT NULL
            OR p_process_terminated IS NOT NULL
            OR p_leader_released IS NOT NULL
            OR p_claim_released IS NOT NULL
            OR p_generation_fenced IS NOT NULL
            OR p_receipt_digest IS NOT NULL
            OR NOT cloud_agents.is_valid_identifier(p_rejection_reason)))
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'retirement-receipt transition input is invalid';
    END IF;

    PERFORM cloud_agents.compatibility_recovery_transition_lock_v2(
        p_tenant_id, p_transition_digest
    );
    identity_digest := cloud_agents.compatibility_recovery_identity_digest_v2(
        'retirement-receipt/v2', p_tenant_id, p_service_kind, p_instance_id,
        p_incarnation::text, p_rollout_generation::text
    );
    PERFORM cloud_agents.compatibility_recovery_lock_v2(
        p_tenant_id,
        'live-instance/v2',
        cloud_agents.compatibility_recovery_identity_digest_v2(
            'live-instance-lock/v2', p_tenant_id, p_service_kind,
            p_instance_id, NULL, NULL
        )
    );
    PERFORM cloud_agents.compatibility_recovery_lock_v2(
        p_tenant_id, expected_profile_id, identity_digest
    );
    SELECT fact.*
    INTO stored_fact
    FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
    WHERE fact.tenant_id = p_tenant_id
        AND fact.transition_digest = p_transition_digest
    FOR UPDATE;
    IF FOUND THEN
        IF stored_fact.operation_id = operation_id
            AND stored_fact.profile_id = expected_profile_id
            AND stored_fact.profile_digest = expected_profile_digest
            AND stored_fact.identity_digest = identity_digest
            AND stored_fact.request_digest = p_request_digest
            AND stored_fact.actor_principal = actor_principal
        THEN
            result_code := 'observed';
            state := stored_fact.state;
            version := stored_fact.version;
            writer_epoch := stored_fact.writer_epoch;
            database_timestamp := stored_fact.database_timestamp;
            stable_error_code := stored_fact.stable_error_code;
            SELECT receipt.credential_revoked, receipt.endpoint_revoked,
                receipt.process_terminated, receipt.leader_released,
                receipt.claim_released, receipt.generation_fenced,
                receipt.receipt_digest
            INTO credential_revoked, endpoint_revoked, process_terminated,
                leader_released, claim_released, generation_fenced,
                receipt_digest
            FROM cloud_agents.compatibility_recovery_retirement_receipts_v2 AS receipt
            WHERE receipt.tenant_id = p_tenant_id
                AND receipt.service_kind = p_service_kind
                AND receipt.instance_id = p_instance_id
                AND receipt.incarnation = p_incarnation
                AND receipt.rollout_generation = p_rollout_generation;
        ELSE
            result_code := 'conflict';
            state := 'unknown';
            version := 0;
            writer_epoch := 0;
            database_timestamp := transitioned_at;
            stable_error_code := 'transition_digest_conflict';
            credential_revoked := false;
            endpoint_revoked := false;
            process_terminated := false;
            leader_released := false;
            claim_released := false;
            generation_fenced := false;
            receipt_digest := NULL;
        END IF;
        write_applied := false;
        reconcile_required := false;
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT receipt.*
    INTO stored
    FROM cloud_agents.compatibility_recovery_retirement_receipts_v2 AS receipt
    WHERE receipt.tenant_id = p_tenant_id
        AND receipt.service_kind = p_service_kind
        AND receipt.instance_id = p_instance_id
        AND receipt.incarnation = p_incarnation
        AND receipt.rollout_generation = p_rollout_generation
    FOR UPDATE;
    had_row := FOUND;

    SELECT instance.*
    INTO live_instance
    FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
    WHERE instance.tenant_id = p_tenant_id
        AND instance.service_kind = p_service_kind
        AND instance.instance_id = p_instance_id
        AND instance.incarnation = p_incarnation
        AND instance.rollout_generation = p_rollout_generation
        AND instance.writer_epoch = p_writer_epoch
    FOR SHARE;
    IF NOT FOUND THEN
        result_code := 'rejected';
        stable_error_code := 'retirement_instance_fence_stale';
    ELSIF p_action = 'collect'
        AND p_generation_fenced
        AND live_instance.drain_state <> 'fenced'
    THEN
        result_code := 'rejected';
        stable_error_code := 'retirement_generation_fence_contradiction';
    ELSIF p_action = 'collect' AND NOT had_row THEN
        IF p_expected_version <> 0 THEN
            result_code := 'rejected';
            stable_error_code := 'retirement_receipt_fence_stale';
        ELSE
            INSERT INTO cloud_agents.compatibility_recovery_retirement_receipts_v2 (
                tenant_id, service_kind, instance_id, incarnation,
                rollout_generation, writer_epoch, state,
                credential_revoked, endpoint_revoked, process_terminated,
                leader_released, claim_released, generation_fenced,
                receipt_digest, rejection_reason, version,
                registry_digest, state_machine_digest, policy_digest,
                profile_id, profile_digest, schema_head, schema_catalog_digest,
                schema_migration_digest, last_transition_digest,
                created_at, updated_at
            ) VALUES (
                p_tenant_id, p_service_kind, p_instance_id, p_incarnation,
                p_rollout_generation, p_writer_epoch, 'collecting',
                p_credential_revoked, p_endpoint_revoked, p_process_terminated,
                p_leader_released, p_claim_released, p_generation_fenced,
                NULL, NULL, 1,
                cloud_agents.compatibility_recovery_registry_digest_v2(),
                cloud_agents.compatibility_recovery_state_machine_digest_v2(),
                cloud_agents.compatibility_recovery_policy_digest_v2(),
                expected_profile_id, expected_profile_digest,
                cloud_agents.compatibility_recovery_schema_head_v2(),
                cloud_agents.compatibility_recovery_schema_catalog_digest_v2(),
                cloud_agents.compatibility_recovery_schema_migration_digest_v2(),
                p_transition_digest, transitioned_at, transitioned_at
            );
            state := 'collecting';
            version := 1;
            writer_epoch := p_writer_epoch;
            credential_revoked := p_credential_revoked;
            endpoint_revoked := p_endpoint_revoked;
            process_terminated := p_process_terminated;
            leader_released := p_leader_released;
            claim_released := p_claim_released;
            generation_fenced := p_generation_fenced;
            receipt_digest := NULL;
        END IF;
    ELSIF NOT had_row THEN
        result_code := 'rejected';
        stable_error_code := 'retirement_receipt_absent';
    ELSIF stored.writer_epoch <> p_writer_epoch
        OR stored.version <> p_expected_version
    THEN
        result_code := 'rejected';
        stable_error_code := 'retirement_receipt_fence_stale';
    ELSIF stored.state <> 'collecting' THEN
        result_code := 'rejected';
        stable_error_code := 'retirement_receipt_state_conflict';
    ELSIF p_action = 'collect' THEN
        UPDATE cloud_agents.compatibility_recovery_retirement_receipts_v2 AS receipt
        SET
            credential_revoked = receipt.credential_revoked
                OR p_credential_revoked,
            endpoint_revoked = receipt.endpoint_revoked OR p_endpoint_revoked,
            process_terminated = receipt.process_terminated
                OR p_process_terminated,
            leader_released = receipt.leader_released OR p_leader_released,
            claim_released = receipt.claim_released OR p_claim_released,
            generation_fenced = receipt.generation_fenced OR p_generation_fenced,
            version = receipt.version + 1,
            last_transition_digest = p_transition_digest,
            updated_at = transitioned_at
        WHERE receipt.tenant_id = p_tenant_id
            AND receipt.service_kind = p_service_kind
            AND receipt.instance_id = p_instance_id
            AND receipt.incarnation = p_incarnation
            AND receipt.rollout_generation = p_rollout_generation
        RETURNING receipt.state, receipt.version, receipt.writer_epoch,
            receipt.credential_revoked, receipt.endpoint_revoked,
            receipt.process_terminated, receipt.leader_released,
            receipt.claim_released, receipt.generation_fenced,
            receipt.receipt_digest
        INTO state, version, writer_epoch, credential_revoked,
            endpoint_revoked, process_terminated, leader_released,
            claim_released, generation_fenced, receipt_digest;
    ELSIF p_action = 'complete' AND live_instance.drain_state <> 'fenced' THEN
        result_code := 'rejected';
        stable_error_code := 'retirement_instance_not_fenced';
    ELSIF p_action = 'complete' AND NOT (
        stored.credential_revoked AND stored.endpoint_revoked
        AND stored.process_terminated AND stored.leader_released
        AND stored.claim_released AND stored.generation_fenced
    ) THEN
        result_code := 'rejected';
        stable_error_code := 'retirement_receipt_incomplete';
    ELSE
        UPDATE cloud_agents.compatibility_recovery_retirement_receipts_v2 AS receipt
        SET
            state = CASE p_action
                WHEN 'complete' THEN 'complete'
                ELSE 'rejected'
            END,
            receipt_digest = CASE
                WHEN p_action = 'complete' THEN p_receipt_digest
                ELSE NULL
            END,
            rejection_reason = CASE
                WHEN p_action = 'reject' THEN p_rejection_reason
                ELSE NULL
            END,
            version = receipt.version + 1,
            last_transition_digest = p_transition_digest,
            updated_at = transitioned_at
        WHERE receipt.tenant_id = p_tenant_id
            AND receipt.service_kind = p_service_kind
            AND receipt.instance_id = p_instance_id
            AND receipt.incarnation = p_incarnation
            AND receipt.rollout_generation = p_rollout_generation
        RETURNING receipt.state, receipt.version, receipt.writer_epoch,
            receipt.credential_revoked, receipt.endpoint_revoked,
            receipt.process_terminated, receipt.leader_released,
            receipt.claim_released, receipt.generation_fenced,
            receipt.receipt_digest
        INTO state, version, writer_epoch, credential_revoked,
            endpoint_revoked, process_terminated, leader_released,
            claim_released, generation_fenced, receipt_digest;
    END IF;

    IF result_code = 'rejected' THEN
        state := CASE WHEN had_row THEN stored.state ELSE 'absent' END;
        version := CASE WHEN had_row THEN stored.version ELSE 0 END;
        writer_epoch := CASE
            WHEN had_row THEN stored.writer_epoch ELSE p_writer_epoch
        END;
        credential_revoked := CASE
            WHEN had_row THEN stored.credential_revoked ELSE false
        END;
        endpoint_revoked := CASE
            WHEN had_row THEN stored.endpoint_revoked ELSE false
        END;
        process_terminated := CASE
            WHEN had_row THEN stored.process_terminated ELSE false
        END;
        leader_released := CASE
            WHEN had_row THEN stored.leader_released ELSE false
        END;
        claim_released := CASE
            WHEN had_row THEN stored.claim_released ELSE false
        END;
        generation_fenced := CASE
            WHEN had_row THEN stored.generation_fenced ELSE false
        END;
        receipt_digest := CASE
            WHEN had_row THEN stored.receipt_digest ELSE NULL
        END;
        write_applied := false;
        reconcile_required := false;
        database_timestamp := transitioned_at;
        RETURN NEXT;
        RETURN;
    END IF;

    PERFORM cloud_agents.compatibility_recovery_record_transition_v2(
        p_tenant_id, operation_id, p_transition_digest,
        expected_profile_id, expected_profile_digest, identity_digest,
        p_request_digest, actor_principal, state, version, writer_epoch,
        NULL, transitioned_at
    );
    result_code := 'applied';
    write_applied := true;
    reconcile_required := false;
    database_timestamp := transitioned_at;
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_live_instance_transition_v2(
    p_action text,
    p_tenant_id text,
    p_service_kind text,
    p_instance_id text,
    p_incarnation bigint,
    p_rollout_generation bigint,
    p_expected_writer_epoch bigint,
    p_new_writer_epoch bigint,
    p_binary_version text,
    p_supported_schema_min text,
    p_supported_schema_max text,
    p_heartbeat_ttl_seconds integer,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text,
    heartbeat_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    expected_profile_id constant text := 'live-instance/v2';
    expected_profile_digest constant text := 'sha256:0b2362b300f48a58160d5f9b754c865194f2a4ca14d6012fe361b340dd1b8ff8';
    operation_id text;
    actor_principal text;
    identity_digest text;
    transitioned_at timestamptz;
    stored record;
    latest record;
    stored_fact record;
    had_row boolean;
    latest_found boolean;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_runtime'
    );
    transitioned_at := pg_catalog.transaction_timestamp();
    operation_id := CASE p_action
        WHEN 'register' THEN 'live-instance-register/v2'
        WHEN 'activate' THEN 'live-instance-activate/v2'
        WHEN 'heartbeat' THEN 'live-instance-heartbeat/v2'
        WHEN 'begin_drain' THEN 'live-instance-begin-drain/v2'
        WHEN 'finish_drain' THEN 'live-instance-finish-drain/v2'
        WHEN 'fence' THEN 'live-instance-fence/v2'
        ELSE NULL
    END;
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_service_kind)
        OR NOT cloud_agents.is_valid_identifier(p_instance_id)
        OR p_incarnation < 1
        OR p_rollout_generation < 1
        OR operation_id IS NULL
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.compatibility_recovery_profile_is_registered_v2(
            expected_profile_id, expected_profile_digest)
        OR (p_action = 'register' AND (
            p_expected_writer_epoch IS NOT NULL
            OR p_new_writer_epoch < 1
            OR pg_catalog.octet_length(p_binary_version) NOT BETWEEN 1 AND 128
            OR p_supported_schema_min !~ '^[0-9]{6}$'
            OR p_supported_schema_max !~ '^[0-9]{6}$'
            OR p_supported_schema_min > p_supported_schema_max
            OR p_heartbeat_ttl_seconds NOT BETWEEN 1 AND 300))
        OR (p_action <> 'register' AND (
            p_expected_writer_epoch < 1
            OR p_binary_version IS NOT NULL
            OR p_supported_schema_min IS NOT NULL
            OR p_supported_schema_max IS NOT NULL))
        OR (p_action IN ('activate', 'begin_drain', 'finish_drain', 'fence') AND (
            p_new_writer_epoch <= p_expected_writer_epoch
            OR p_heartbeat_ttl_seconds IS NOT NULL))
        OR (p_action = 'heartbeat' AND (
            p_new_writer_epoch IS NOT NULL
            OR p_heartbeat_ttl_seconds NOT BETWEEN 1 AND 300))
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'live-instance transition input is invalid';
    END IF;

    PERFORM cloud_agents.compatibility_recovery_transition_lock_v2(
        p_tenant_id, p_transition_digest
    );
    identity_digest := cloud_agents.compatibility_recovery_identity_digest_v2(
        'live-instance/v2', p_tenant_id, p_service_kind, p_instance_id,
        p_incarnation::text, p_rollout_generation::text
    );
    PERFORM cloud_agents.compatibility_recovery_lock_v2(
        p_tenant_id,
        expected_profile_id,
        cloud_agents.compatibility_recovery_identity_digest_v2(
            'live-instance-lock/v2', p_tenant_id, p_service_kind, p_instance_id,
            NULL, NULL
        )
    );
    SELECT fact.*
    INTO stored_fact
    FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
    WHERE fact.tenant_id = p_tenant_id
        AND fact.transition_digest = p_transition_digest
    FOR UPDATE;
    IF FOUND THEN
        IF stored_fact.operation_id = operation_id
            AND stored_fact.profile_id = expected_profile_id
            AND stored_fact.profile_digest = expected_profile_digest
            AND stored_fact.identity_digest = identity_digest
            AND stored_fact.request_digest = p_request_digest
            AND stored_fact.actor_principal = actor_principal
        THEN
            result_code := 'observed';
            state := stored_fact.state;
            version := stored_fact.version;
            writer_epoch := stored_fact.writer_epoch;
            database_timestamp := stored_fact.database_timestamp;
            stable_error_code := stored_fact.stable_error_code;
            SELECT instance.heartbeat_at
            INTO heartbeat_at
            FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
            WHERE instance.tenant_id = p_tenant_id
                AND instance.service_kind = p_service_kind
                AND instance.instance_id = p_instance_id
                AND instance.incarnation = p_incarnation;
        ELSE
            result_code := 'conflict';
            state := 'unknown';
            version := 0;
            writer_epoch := 0;
            database_timestamp := transitioned_at;
            stable_error_code := 'transition_digest_conflict';
            heartbeat_at := NULL;
        END IF;
        write_applied := false;
        reconcile_required := false;
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT instance.*
    INTO stored
    FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
    WHERE instance.tenant_id = p_tenant_id
        AND instance.service_kind = p_service_kind
        AND instance.instance_id = p_instance_id
        AND instance.incarnation = p_incarnation
    FOR UPDATE;
    had_row := FOUND;

    IF p_action = 'register' THEN
        SELECT instance.*
        INTO latest
        FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
        WHERE instance.tenant_id = p_tenant_id
            AND instance.service_kind = p_service_kind
            AND instance.instance_id = p_instance_id
        ORDER BY instance.incarnation DESC, instance.rollout_generation DESC
        LIMIT 1
        FOR UPDATE;
        latest_found := FOUND;
        IF had_row
            OR (latest_found AND (
                latest.incarnation >= p_incarnation
                OR latest.rollout_generation > p_rollout_generation))
        THEN
            result_code := 'rejected';
            stable_error_code := 'live_instance_generation_stale';
        ELSE
            INSERT INTO cloud_agents.compatibility_recovery_live_instances_v2 (
                tenant_id, service_kind, instance_id, incarnation,
                rollout_generation, writer_epoch, binary_version,
                supported_schema_min, supported_schema_max, drain_state,
                heartbeat_at, heartbeat_ttl_seconds, version,
                registry_digest, state_machine_digest, policy_digest,
                profile_id, profile_digest, schema_head, schema_catalog_digest,
                schema_migration_digest, last_transition_digest,
                created_at, updated_at
            ) VALUES (
                p_tenant_id, p_service_kind, p_instance_id, p_incarnation,
                p_rollout_generation, p_new_writer_epoch, p_binary_version,
                p_supported_schema_min, p_supported_schema_max, 'registered',
                transitioned_at, p_heartbeat_ttl_seconds, 1,
                cloud_agents.compatibility_recovery_registry_digest_v2(),
                cloud_agents.compatibility_recovery_state_machine_digest_v2(),
                cloud_agents.compatibility_recovery_policy_digest_v2(),
                expected_profile_id, expected_profile_digest,
                cloud_agents.compatibility_recovery_schema_head_v2(),
                cloud_agents.compatibility_recovery_schema_catalog_digest_v2(),
                cloud_agents.compatibility_recovery_schema_migration_digest_v2(),
                p_transition_digest, transitioned_at, transitioned_at
            );
            state := 'registered';
            version := 1;
            writer_epoch := p_new_writer_epoch;
            heartbeat_at := transitioned_at;
        END IF;
    ELSIF NOT had_row
        OR stored.rollout_generation <> p_rollout_generation
        OR stored.writer_epoch <> p_expected_writer_epoch
    THEN
        result_code := 'rejected';
        stable_error_code := 'live_instance_fence_stale';
    ELSIF p_action = 'activate' AND stored.drain_state <> 'registered' THEN
        result_code := 'rejected';
        stable_error_code := 'live_instance_state_conflict';
    ELSIF p_action = 'begin_drain' AND stored.drain_state <> 'active' THEN
        result_code := 'rejected';
        stable_error_code := 'live_instance_state_conflict';
    ELSIF p_action = 'finish_drain' AND stored.drain_state <> 'draining' THEN
        result_code := 'rejected';
        stable_error_code := 'live_instance_state_conflict';
    ELSIF p_action = 'fence' AND stored.drain_state = 'fenced' THEN
        result_code := 'rejected';
        stable_error_code := 'live_instance_state_conflict';
    ELSIF p_action = 'heartbeat'
        AND (
            stored.drain_state NOT IN ('active', 'draining')
            OR stored.heartbeat_at
                + pg_catalog.make_interval(secs => stored.heartbeat_ttl_seconds)
                <= transitioned_at
        )
    THEN
        result_code := 'rejected';
        stable_error_code := 'live_instance_heartbeat_stale';
    ELSE
        UPDATE cloud_agents.compatibility_recovery_live_instances_v2 AS instance
        SET
            drain_state = CASE p_action
                WHEN 'activate' THEN 'active'
                WHEN 'begin_drain' THEN 'draining'
                WHEN 'finish_drain' THEN 'drained'
                WHEN 'fence' THEN 'fenced'
                ELSE instance.drain_state
            END,
            writer_epoch = CASE
                WHEN p_action = 'heartbeat' THEN instance.writer_epoch
                ELSE p_new_writer_epoch
            END,
            heartbeat_at = CASE
                WHEN p_action = 'heartbeat' THEN transitioned_at
                ELSE instance.heartbeat_at
            END,
            heartbeat_ttl_seconds = CASE
                WHEN p_action = 'heartbeat' THEN p_heartbeat_ttl_seconds
                ELSE instance.heartbeat_ttl_seconds
            END,
            version = instance.version + 1,
            last_transition_digest = p_transition_digest,
            updated_at = transitioned_at
        WHERE instance.tenant_id = p_tenant_id
            AND instance.service_kind = p_service_kind
            AND instance.instance_id = p_instance_id
            AND instance.incarnation = p_incarnation
        RETURNING instance.drain_state, instance.version,
            instance.writer_epoch, instance.heartbeat_at
        INTO state, version, writer_epoch, heartbeat_at;
    END IF;

    IF result_code = 'rejected' THEN
        state := CASE WHEN had_row THEN stored.drain_state ELSE 'absent' END;
        version := CASE WHEN had_row THEN stored.version ELSE 0 END;
        writer_epoch := CASE WHEN had_row THEN stored.writer_epoch ELSE 0 END;
        heartbeat_at := CASE WHEN had_row THEN stored.heartbeat_at ELSE NULL END;
        write_applied := false;
        reconcile_required := false;
        database_timestamp := transitioned_at;
        RETURN NEXT;
        RETURN;
    END IF;

    PERFORM cloud_agents.compatibility_recovery_record_transition_v2(
        p_tenant_id, operation_id, p_transition_digest,
        expected_profile_id, expected_profile_digest, identity_digest,
        p_request_digest, actor_principal, state, version, writer_epoch,
        NULL, transitioned_at
    );
    result_code := 'applied';
    write_applied := true;
    reconcile_required := false;
    database_timestamp := transitioned_at;
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_backfill_start_v2(
    p_tenant_id text,
    p_migration_id text,
    p_phase text,
    p_cursor text,
    p_digest text,
    p_writer_epoch bigint,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    lease_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    RETURN QUERY
    SELECT * FROM cloud_agents.compatibility_recovery_backfill_transition_v2(
        'start', p_tenant_id, p_migration_id, p_phase, p_cursor, p_digest, 0,
        NULL, NULL, p_writer_epoch, NULL, p_transition_digest, p_request_digest
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_backfill_acquire_lease_v2(
    p_tenant_id text,
    p_migration_id text,
    p_lease_owner text,
    p_expected_writer_epoch bigint,
    p_new_writer_epoch bigint,
    p_lease_seconds integer,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    lease_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    RETURN QUERY
    SELECT * FROM cloud_agents.compatibility_recovery_backfill_transition_v2(
        'acquire_lease', p_tenant_id, p_migration_id, NULL, NULL, NULL, NULL,
        p_lease_owner, p_expected_writer_epoch, p_new_writer_epoch,
        p_lease_seconds, p_transition_digest, p_request_digest
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_backfill_advance_v2(
    p_tenant_id text,
    p_migration_id text,
    p_lease_owner text,
    p_writer_epoch bigint,
    p_phase text,
    p_cursor text,
    p_digest text,
    p_count bigint,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    lease_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    RETURN QUERY
    SELECT * FROM cloud_agents.compatibility_recovery_backfill_transition_v2(
        'advance', p_tenant_id, p_migration_id, p_phase, p_cursor, p_digest,
        p_count, p_lease_owner, p_writer_epoch, NULL, NULL,
        p_transition_digest, p_request_digest
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_backfill_heartbeat_v2(
    p_tenant_id text,
    p_migration_id text,
    p_lease_owner text,
    p_writer_epoch bigint,
    p_lease_seconds integer,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    lease_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    RETURN QUERY
    SELECT * FROM cloud_agents.compatibility_recovery_backfill_transition_v2(
        'heartbeat', p_tenant_id, p_migration_id, NULL, NULL, NULL, NULL,
        p_lease_owner, p_writer_epoch, NULL, p_lease_seconds,
        p_transition_digest, p_request_digest
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_backfill_complete_v2(
    p_tenant_id text,
    p_migration_id text,
    p_lease_owner text,
    p_writer_epoch bigint,
    p_phase text,
    p_cursor text,
    p_digest text,
    p_count bigint,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    lease_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    RETURN QUERY
    SELECT * FROM cloud_agents.compatibility_recovery_backfill_transition_v2(
        'complete', p_tenant_id, p_migration_id, p_phase, p_cursor, p_digest,
        p_count, p_lease_owner, p_writer_epoch, NULL, NULL,
        p_transition_digest, p_request_digest
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_backfill_reconcile_v2(
    p_tenant_id text,
    p_migration_id text,
    p_transition_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    lease_owner text, lease_expires_at timestamptz,
    phase text, cursor text, digest text, count bigint,
    transition_observed boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    observed record;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_migration_owner'
    );
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_migration_id !~ '^[0-9]{6}$'
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'backfill reconcile input is invalid';
    END IF;
    SELECT backfill.*
    INTO observed
    FROM cloud_agents.compatibility_recovery_backfills_v2 AS backfill
    WHERE backfill.tenant_id = p_tenant_id
        AND backfill.migration_id = p_migration_id;
    IF FOUND THEN
        result_code := 'observed';
        state := observed.state;
        version := observed.version;
        writer_epoch := observed.writer_epoch;
        lease_owner := observed.lease_owner;
        lease_expires_at := observed.lease_expires_at;
        phase := observed.phase;
        cursor := observed.cursor;
        digest := observed.digest;
        count := observed.count;
    ELSE
        result_code := 'not_observed';
        state := 'absent';
        version := 0;
        writer_epoch := 0;
        lease_owner := NULL;
        lease_expires_at := NULL;
        phase := NULL;
        cursor := NULL;
        digest := NULL;
        count := 0;
    END IF;
    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
        WHERE fact.tenant_id = p_tenant_id
            AND fact.transition_digest = p_transition_digest
            AND fact.profile_id = 'backfill/v2'
            AND fact.identity_digest =
                cloud_agents.compatibility_recovery_identity_digest_v2(
                    'backfill/v2', p_tenant_id, p_migration_id,
                    NULL, NULL, NULL
                )
    ) INTO transition_observed;
    write_applied := false;
    reconcile_required := false;
    database_timestamp := pg_catalog.transaction_timestamp();
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_record_transition_v2(
    p_tenant_id text,
    p_operation_id text,
    p_transition_digest text,
    p_profile_id text,
    p_profile_digest text,
    p_identity_digest text,
    p_request_digest text,
    p_actor_principal text,
    p_state text,
    p_version bigint,
    p_writer_epoch bigint,
    p_stable_error_code text,
    p_database_timestamp timestamptz
)
RETURNS void
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    INSERT INTO cloud_agents.compatibility_recovery_transition_facts_v2 (
        tenant_id,
        operation_id,
        transition_digest,
        registry_digest,
        state_machine_digest,
        policy_digest,
        profile_id,
        profile_digest,
        schema_head,
        schema_catalog_digest,
        schema_migration_digest,
        identity_digest,
        request_digest,
        actor_principal,
        result_code,
        state,
        version,
        writer_epoch,
        stable_error_code,
        database_timestamp
    ) VALUES (
        p_tenant_id,
        p_operation_id,
        p_transition_digest,
        cloud_agents.compatibility_recovery_registry_digest_v2(),
        cloud_agents.compatibility_recovery_state_machine_digest_v2(),
        cloud_agents.compatibility_recovery_policy_digest_v2(),
        p_profile_id,
        p_profile_digest,
        cloud_agents.compatibility_recovery_schema_head_v2(),
        cloud_agents.compatibility_recovery_schema_catalog_digest_v2(),
        cloud_agents.compatibility_recovery_schema_migration_digest_v2(),
        p_identity_digest,
        p_request_digest,
        p_actor_principal,
        'applied',
        p_state,
        p_version,
        p_writer_epoch,
        p_stable_error_code,
        p_database_timestamp
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_workload_principal_transition_v2(
    p_action text,
    p_tenant_id text,
    p_workload_id text,
    p_provider text,
    p_expected_principal_id text,
    p_new_principal_id text,
    p_expected_epoch bigint,
    p_new_epoch bigint,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    expected_profile_id constant text := 'workload-principal/v2';
    expected_profile_digest constant text := 'sha256:7208b25e051ce6cb298d8f88190365a950bc0ac48a669fbf7ab93de35cee6878';
    operation_id text;
    actor_principal text;
    identity_digest text;
    transitioned_at timestamptz;
    stored record;
    stored_fact record;
    had_row boolean;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_bootstrap_admin'
    );
    transitioned_at := pg_catalog.transaction_timestamp();
    operation_id := CASE p_action
        WHEN 'register' THEN 'workload-principal-register/v2'
        WHEN 'rotate' THEN 'workload-principal-rotate/v2'
        WHEN 'revoke' THEN 'workload-principal-revoke/v2'
        ELSE NULL
    END;
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR operation_id IS NULL
        OR NOT cloud_agents.is_valid_identifier(p_workload_id)
        OR NOT cloud_agents.is_valid_identifier(p_provider)
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.compatibility_recovery_profile_is_registered_v2(
            expected_profile_id, expected_profile_digest)
        OR (p_action = 'register' AND (
            p_expected_principal_id IS NOT NULL
            OR p_expected_epoch IS NOT NULL
            OR NOT cloud_agents.is_valid_identifier(p_new_principal_id)
            OR p_new_epoch < 1))
        OR (p_action = 'rotate' AND (
            NOT cloud_agents.is_valid_identifier(p_expected_principal_id)
            OR NOT cloud_agents.is_valid_identifier(p_new_principal_id)
            OR p_expected_epoch < 1
            OR p_new_epoch <= p_expected_epoch))
        OR (p_action = 'revoke' AND (
            NOT cloud_agents.is_valid_identifier(p_expected_principal_id)
            OR p_new_principal_id IS NOT NULL
            OR p_expected_epoch < 1
            OR p_new_epoch IS NOT NULL))
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'workload-principal transition input is invalid';
    END IF;

    PERFORM cloud_agents.compatibility_recovery_transition_lock_v2(
        p_tenant_id, p_transition_digest
    );
    identity_digest := cloud_agents.compatibility_recovery_identity_digest_v2(
        'workload-principal/v2',
        p_tenant_id,
        p_workload_id,
        p_provider,
        NULL,
        NULL
    );
    PERFORM cloud_agents.compatibility_recovery_lock_v2(
        p_tenant_id, expected_profile_id, identity_digest
    );

    SELECT fact.*
    INTO stored_fact
    FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
    WHERE fact.tenant_id = p_tenant_id
        AND fact.transition_digest = p_transition_digest
    FOR UPDATE;
    IF FOUND THEN
        IF stored_fact.operation_id = operation_id
            AND stored_fact.profile_id = expected_profile_id
            AND stored_fact.profile_digest = expected_profile_digest
            AND stored_fact.identity_digest = identity_digest
            AND stored_fact.request_digest = p_request_digest
            AND stored_fact.actor_principal = actor_principal
        THEN
            result_code := 'observed';
            write_applied := false;
            reconcile_required := false;
            state := stored_fact.state;
            version := stored_fact.version;
            writer_epoch := stored_fact.writer_epoch;
            database_timestamp := stored_fact.database_timestamp;
            stable_error_code := stored_fact.stable_error_code;
        ELSE
            result_code := 'conflict';
            write_applied := false;
            reconcile_required := false;
            state := 'unknown';
            version := 0;
            writer_epoch := 0;
            database_timestamp := transitioned_at;
            stable_error_code := 'transition_digest_conflict';
        END IF;
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT principal.*
    INTO stored
    FROM cloud_agents.compatibility_recovery_workload_principals_v2 AS principal
    WHERE principal.tenant_id = p_tenant_id
        AND principal.workload_id = p_workload_id
        AND principal.provider = p_provider
    FOR UPDATE;
    had_row := FOUND;

    IF p_action = 'register' THEN
        IF had_row THEN
            result_code := 'rejected';
            write_applied := false;
            reconcile_required := false;
            state := stored.state;
            version := stored.version;
            writer_epoch := stored.epoch;
            database_timestamp := transitioned_at;
            stable_error_code := 'principal_already_registered';
            RETURN NEXT;
            RETURN;
        END IF;
        INSERT INTO cloud_agents.compatibility_recovery_workload_principals_v2 (
            tenant_id, workload_id, provider, principal_id, epoch, state, version,
            registry_digest, state_machine_digest, policy_digest,
            profile_id, profile_digest, schema_head, schema_catalog_digest,
            schema_migration_digest, last_transition_digest,
            created_at, updated_at, revoked_at
        ) VALUES (
            p_tenant_id, p_workload_id, p_provider, p_new_principal_id,
            p_new_epoch, 'active', 1,
            cloud_agents.compatibility_recovery_registry_digest_v2(),
            cloud_agents.compatibility_recovery_state_machine_digest_v2(),
            cloud_agents.compatibility_recovery_policy_digest_v2(),
            expected_profile_id, expected_profile_digest,
            cloud_agents.compatibility_recovery_schema_head_v2(),
            cloud_agents.compatibility_recovery_schema_catalog_digest_v2(),
            cloud_agents.compatibility_recovery_schema_migration_digest_v2(),
            p_transition_digest, transitioned_at, transitioned_at, NULL
        );
        state := 'active';
        version := 1;
        writer_epoch := p_new_epoch;
    ELSIF NOT had_row
        OR stored.state <> 'active'
        OR stored.principal_id <> p_expected_principal_id
        OR stored.epoch <> p_expected_epoch
    THEN
        result_code := 'rejected';
        write_applied := false;
        reconcile_required := false;
        state := CASE WHEN had_row THEN stored.state ELSE 'absent' END;
        version := CASE WHEN had_row THEN stored.version ELSE 0 END;
        writer_epoch := CASE WHEN had_row THEN stored.epoch ELSE 0 END;
        database_timestamp := transitioned_at;
        stable_error_code := 'principal_fence_stale';
        RETURN NEXT;
        RETURN;
    ELSIF p_action = 'rotate' THEN
        UPDATE cloud_agents.compatibility_recovery_workload_principals_v2 AS principal
        SET
            principal_id = p_new_principal_id,
            epoch = p_new_epoch,
            version = principal.version + 1,
            last_transition_digest = p_transition_digest,
            updated_at = transitioned_at
        WHERE principal.tenant_id = p_tenant_id
            AND principal.workload_id = p_workload_id
            AND principal.provider = p_provider
        RETURNING principal.state, principal.version, principal.epoch
        INTO state, version, writer_epoch;
    ELSE
        UPDATE cloud_agents.compatibility_recovery_workload_principals_v2 AS principal
        SET
            state = 'revoked',
            version = principal.version + 1,
            last_transition_digest = p_transition_digest,
            updated_at = transitioned_at,
            revoked_at = transitioned_at
        WHERE principal.tenant_id = p_tenant_id
            AND principal.workload_id = p_workload_id
            AND principal.provider = p_provider
        RETURNING principal.state, principal.version, principal.epoch
        INTO state, version, writer_epoch;
    END IF;

    PERFORM cloud_agents.compatibility_recovery_record_transition_v2(
        p_tenant_id, operation_id, p_transition_digest,
        expected_profile_id, expected_profile_digest, identity_digest,
        p_request_digest, actor_principal, state, version, writer_epoch,
        NULL, transitioned_at
    );
    result_code := 'applied';
    write_applied := true;
    reconcile_required := false;
    database_timestamp := transitioned_at;
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_workload_principal_register_v2(
    p_tenant_id text,
    p_workload_id text,
    p_provider text,
    p_principal_id text,
    p_epoch bigint,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.compatibility_recovery_workload_principal_transition_v2(
        'register', p_tenant_id, p_workload_id, p_provider, NULL, p_principal_id,
        NULL, p_epoch, p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_workload_principal_rotate_v2(
    p_tenant_id text,
    p_workload_id text,
    p_provider text,
    p_expected_principal_id text,
    p_new_principal_id text,
    p_expected_epoch bigint,
    p_new_epoch bigint,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.compatibility_recovery_workload_principal_transition_v2(
        'rotate', p_tenant_id, p_workload_id, p_provider,
        p_expected_principal_id, p_new_principal_id,
        p_expected_epoch, p_new_epoch, p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_workload_principal_revoke_v2(
    p_tenant_id text,
    p_workload_id text,
    p_provider text,
    p_expected_principal_id text,
    p_expected_epoch bigint,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.compatibility_recovery_workload_principal_transition_v2(
        'revoke', p_tenant_id, p_workload_id, p_provider,
        p_expected_principal_id, NULL, p_expected_epoch, NULL,
        p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_workload_principal_reconcile_v2(
    p_tenant_id text,
    p_workload_id text,
    p_provider text,
    p_transition_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text,
    principal_id text,
    transition_observed boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    observed record;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_bootstrap_admin'
    );
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_workload_id)
        OR NOT cloud_agents.is_valid_identifier(p_provider)
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'workload-principal reconcile input is invalid';
    END IF;
    SELECT principal.*
    INTO observed
    FROM cloud_agents.compatibility_recovery_workload_principals_v2 AS principal
    WHERE principal.tenant_id = p_tenant_id
        AND principal.workload_id = p_workload_id
        AND principal.provider = p_provider;
    IF FOUND THEN
        result_code := 'observed';
        state := observed.state;
        version := observed.version;
        writer_epoch := observed.epoch;
        principal_id := observed.principal_id;
    ELSE
        result_code := 'not_observed';
        state := 'absent';
        version := 0;
        writer_epoch := 0;
        principal_id := NULL;
    END IF;
    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
        WHERE fact.tenant_id = p_tenant_id
            AND fact.transition_digest = p_transition_digest
            AND fact.profile_id = 'workload-principal/v2'
            AND fact.identity_digest =
                cloud_agents.compatibility_recovery_identity_digest_v2(
                    'workload-principal/v2', p_tenant_id, p_workload_id,
                    p_provider, NULL, NULL
                )
    ) INTO transition_observed;
    write_applied := false;
    reconcile_required := false;
    database_timestamp := pg_catalog.transaction_timestamp();
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_backfill_transition_v2(
    p_action text,
    p_tenant_id text,
    p_migration_id text,
    p_phase text,
    p_cursor text,
    p_digest text,
    p_count bigint,
    p_lease_owner text,
    p_expected_writer_epoch bigint,
    p_new_writer_epoch bigint,
    p_lease_seconds integer,
    p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text,
    lease_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    expected_profile_id constant text := 'backfill/v2';
    expected_profile_digest constant text := 'sha256:c5d96407e0c0003689faa9e5526098b57e8b40d9ef67c76f9318e2b0326e6145';
    operation_id text;
    actor_principal text;
    identity_digest text;
    transitioned_at timestamptz;
    stored record;
    stored_fact record;
    had_row boolean;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_migration_owner'
    );
    transitioned_at := pg_catalog.transaction_timestamp();
    operation_id := CASE p_action
        WHEN 'start' THEN 'backfill-start/v2'
        WHEN 'acquire_lease' THEN 'backfill-acquire-lease/v2'
        WHEN 'advance' THEN 'backfill-advance/v2'
        WHEN 'heartbeat' THEN 'backfill-heartbeat/v2'
        WHEN 'complete' THEN 'backfill-complete/v2'
        ELSE NULL
    END;
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_migration_id !~ '^[0-9]{6}$'
        OR operation_id IS NULL
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.compatibility_recovery_profile_is_registered_v2(
            expected_profile_id, expected_profile_digest)
        OR (p_action = 'start' AND (
            NOT cloud_agents.is_valid_identifier(p_phase)
            OR pg_catalog.octet_length(p_cursor) NOT BETWEEN 1 AND 2048
            OR p_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_count <> 0
            OR p_lease_owner IS NOT NULL
            OR p_expected_writer_epoch IS NOT NULL
            OR p_new_writer_epoch < 1
            OR p_lease_seconds IS NOT NULL))
        OR (p_action = 'acquire_lease' AND (
            p_phase IS NOT NULL OR p_cursor IS NOT NULL OR p_digest IS NOT NULL
            OR p_count IS NOT NULL
            OR NOT cloud_agents.is_valid_identifier(p_lease_owner)
            OR p_expected_writer_epoch < 1
            OR p_new_writer_epoch <= p_expected_writer_epoch
            OR p_lease_seconds NOT BETWEEN 1 AND 60))
        OR (p_action = 'advance' AND (
            NOT cloud_agents.is_valid_identifier(p_phase)
            OR pg_catalog.octet_length(p_cursor) NOT BETWEEN 1 AND 2048
            OR p_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_count < 1
            OR NOT cloud_agents.is_valid_identifier(p_lease_owner)
            OR p_expected_writer_epoch < 1
            OR p_new_writer_epoch IS NOT NULL
            OR p_lease_seconds IS NOT NULL))
        OR (p_action = 'heartbeat' AND (
            p_phase IS NOT NULL OR p_cursor IS NOT NULL OR p_digest IS NOT NULL
            OR p_count IS NOT NULL
            OR NOT cloud_agents.is_valid_identifier(p_lease_owner)
            OR p_expected_writer_epoch < 1
            OR p_new_writer_epoch IS NOT NULL
            OR p_lease_seconds NOT BETWEEN 1 AND 60))
        OR (p_action = 'complete' AND (
            NOT cloud_agents.is_valid_identifier(p_phase)
            OR pg_catalog.octet_length(p_cursor) NOT BETWEEN 1 AND 2048
            OR p_digest !~ '^sha256:[0-9a-f]{64}$'
            OR p_count < 1
            OR NOT cloud_agents.is_valid_identifier(p_lease_owner)
            OR p_expected_writer_epoch < 1
            OR p_new_writer_epoch IS NOT NULL
            OR p_lease_seconds IS NOT NULL))
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'backfill transition input is invalid';
    END IF;

    PERFORM cloud_agents.compatibility_recovery_transition_lock_v2(
        p_tenant_id, p_transition_digest
    );
    identity_digest := cloud_agents.compatibility_recovery_identity_digest_v2(
        'backfill/v2', p_tenant_id, p_migration_id, NULL, NULL, NULL
    );
    PERFORM cloud_agents.compatibility_recovery_lock_v2(
        p_tenant_id, expected_profile_id, identity_digest
    );
    SELECT fact.*
    INTO stored_fact
    FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
    WHERE fact.tenant_id = p_tenant_id
        AND fact.transition_digest = p_transition_digest
    FOR UPDATE;
    IF FOUND THEN
        IF stored_fact.operation_id = operation_id
            AND stored_fact.profile_id = expected_profile_id
            AND stored_fact.profile_digest = expected_profile_digest
            AND stored_fact.identity_digest = identity_digest
            AND stored_fact.request_digest = p_request_digest
            AND stored_fact.actor_principal = actor_principal
        THEN
            result_code := 'observed';
            state := stored_fact.state;
            version := stored_fact.version;
            writer_epoch := stored_fact.writer_epoch;
            database_timestamp := stored_fact.database_timestamp;
            stable_error_code := stored_fact.stable_error_code;
            SELECT backfill.lease_expires_at
            INTO lease_expires_at
            FROM cloud_agents.compatibility_recovery_backfills_v2 AS backfill
            WHERE backfill.tenant_id = p_tenant_id
                AND backfill.migration_id = p_migration_id;
        ELSE
            result_code := 'conflict';
            state := 'unknown';
            version := 0;
            writer_epoch := 0;
            database_timestamp := transitioned_at;
            stable_error_code := 'transition_digest_conflict';
            lease_expires_at := NULL;
        END IF;
        write_applied := false;
        reconcile_required := false;
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT backfill.*
    INTO stored
    FROM cloud_agents.compatibility_recovery_backfills_v2 AS backfill
    WHERE backfill.tenant_id = p_tenant_id
        AND backfill.migration_id = p_migration_id
    FOR UPDATE;
    had_row := FOUND;

    IF p_action = 'start' THEN
        IF had_row THEN
            result_code := 'rejected';
            stable_error_code := 'backfill_already_started';
        ELSE
            INSERT INTO cloud_agents.compatibility_recovery_backfills_v2 (
                tenant_id, migration_id, state, phase, cursor, digest, count,
                writer_epoch, lease_owner, lease_expires_at, version,
                registry_digest, state_machine_digest, policy_digest,
                profile_id, profile_digest, schema_head, schema_catalog_digest,
                schema_migration_digest, last_transition_digest,
                stable_error_code, committed_at, created_at, updated_at
            ) VALUES (
                p_tenant_id, p_migration_id, 'pending', p_phase, p_cursor,
                p_digest, p_count, p_new_writer_epoch, NULL, NULL, 1,
                cloud_agents.compatibility_recovery_registry_digest_v2(),
                cloud_agents.compatibility_recovery_state_machine_digest_v2(),
                cloud_agents.compatibility_recovery_policy_digest_v2(),
                expected_profile_id, expected_profile_digest,
                cloud_agents.compatibility_recovery_schema_head_v2(),
                cloud_agents.compatibility_recovery_schema_catalog_digest_v2(),
                cloud_agents.compatibility_recovery_schema_migration_digest_v2(),
                p_transition_digest, NULL, NULL, transitioned_at, transitioned_at
            );
            state := 'pending';
            version := 1;
            writer_epoch := p_new_writer_epoch;
            lease_expires_at := NULL;
        END IF;
    ELSIF NOT had_row THEN
        result_code := 'rejected';
        stable_error_code := 'backfill_absent';
    ELSIF p_action = 'acquire_lease' THEN
        IF stored.state <> 'pending'
            OR stored.writer_epoch <> p_expected_writer_epoch
        THEN
            result_code := 'rejected';
            stable_error_code := 'backfill_lease_fence_stale';
        ELSE
            UPDATE cloud_agents.compatibility_recovery_backfills_v2 AS backfill
            SET
                state = 'leased',
                writer_epoch = p_new_writer_epoch,
                lease_owner = p_lease_owner,
                lease_expires_at = transitioned_at
                    + pg_catalog.make_interval(secs => p_lease_seconds),
                version = backfill.version + 1,
                last_transition_digest = p_transition_digest,
                updated_at = transitioned_at
            WHERE backfill.tenant_id = p_tenant_id
                AND backfill.migration_id = p_migration_id
            RETURNING backfill.state, backfill.version, backfill.writer_epoch,
                backfill.lease_expires_at
            INTO state, version, writer_epoch, lease_expires_at;
        END IF;
    ELSIF p_action IN ('advance', 'heartbeat', 'complete') AND (
        stored.state NOT IN ('leased', 'running')
        OR stored.writer_epoch <> p_expected_writer_epoch
        OR stored.lease_owner <> p_lease_owner
        OR stored.lease_expires_at <= transitioned_at
    ) THEN
        result_code := 'rejected';
        stable_error_code := 'backfill_lease_fence_stale';
    ELSIF p_action = 'advance' THEN
        IF p_count <= stored.count THEN
            result_code := 'rejected';
            stable_error_code := 'backfill_cursor_not_monotonic';
        ELSE
            UPDATE cloud_agents.compatibility_recovery_backfills_v2 AS backfill
            SET
                state = 'running',
                phase = p_phase,
                cursor = p_cursor,
                digest = p_digest,
                count = p_count,
                version = backfill.version + 1,
                last_transition_digest = p_transition_digest,
                updated_at = transitioned_at
            WHERE backfill.tenant_id = p_tenant_id
                AND backfill.migration_id = p_migration_id
            RETURNING backfill.state, backfill.version, backfill.writer_epoch,
                backfill.lease_expires_at
            INTO state, version, writer_epoch, lease_expires_at;
        END IF;
    ELSIF p_action = 'heartbeat' THEN
        UPDATE cloud_agents.compatibility_recovery_backfills_v2 AS backfill
        SET
            lease_expires_at = transitioned_at
                + pg_catalog.make_interval(secs => p_lease_seconds),
            version = backfill.version + 1,
            last_transition_digest = p_transition_digest,
            updated_at = transitioned_at
        WHERE backfill.tenant_id = p_tenant_id
            AND backfill.migration_id = p_migration_id
        RETURNING backfill.state, backfill.version, backfill.writer_epoch,
            backfill.lease_expires_at
        INTO state, version, writer_epoch, lease_expires_at;
    ELSIF stored.state <> 'running' OR p_count < stored.count THEN
        result_code := 'rejected';
        stable_error_code := 'backfill_completion_invalid';
    ELSE
        UPDATE cloud_agents.compatibility_recovery_backfills_v2 AS backfill
        SET
            state = 'succeeded',
            phase = p_phase,
            cursor = p_cursor,
            digest = p_digest,
            count = p_count,
            lease_owner = NULL,
            lease_expires_at = NULL,
            version = backfill.version + 1,
            last_transition_digest = p_transition_digest,
            committed_at = transitioned_at,
            updated_at = transitioned_at
        WHERE backfill.tenant_id = p_tenant_id
            AND backfill.migration_id = p_migration_id
        RETURNING backfill.state, backfill.version, backfill.writer_epoch,
            backfill.lease_expires_at
        INTO state, version, writer_epoch, lease_expires_at;
    END IF;

    IF result_code = 'rejected' THEN
        state := CASE WHEN had_row THEN stored.state ELSE 'absent' END;
        version := CASE WHEN had_row THEN stored.version ELSE 0 END;
        writer_epoch := CASE WHEN had_row THEN stored.writer_epoch ELSE 0 END;
        lease_expires_at := CASE WHEN had_row THEN stored.lease_expires_at ELSE NULL END;
        write_applied := false;
        reconcile_required := false;
        database_timestamp := transitioned_at;
        RETURN NEXT;
        RETURN;
    END IF;

    PERFORM cloud_agents.compatibility_recovery_record_transition_v2(
        p_tenant_id, operation_id, p_transition_digest,
        expected_profile_id, expected_profile_digest, identity_digest,
        p_request_digest, actor_principal, state, version, writer_epoch,
        NULL, transitioned_at
    );
    result_code := 'applied';
    write_applied := true;
    reconcile_required := false;
    database_timestamp := transitioned_at;
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_live_instance_register_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint, p_writer_epoch bigint,
    p_binary_version text, p_supported_schema_min text,
    p_supported_schema_max text, p_heartbeat_ttl_seconds integer,
    p_transition_digest text, p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    heartbeat_at timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT * FROM cloud_agents.compatibility_recovery_live_instance_transition_v2(
        'register', p_tenant_id, p_service_kind, p_instance_id, p_incarnation,
        p_rollout_generation, NULL, p_writer_epoch, p_binary_version,
        p_supported_schema_min, p_supported_schema_max, p_heartbeat_ttl_seconds,
        p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_live_instance_activate_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint,
    p_expected_writer_epoch bigint, p_new_writer_epoch bigint,
    p_transition_digest text, p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    heartbeat_at timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT * FROM cloud_agents.compatibility_recovery_live_instance_transition_v2(
        'activate', p_tenant_id, p_service_kind, p_instance_id, p_incarnation,
        p_rollout_generation, p_expected_writer_epoch, p_new_writer_epoch,
        NULL, NULL, NULL, NULL, p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_live_instance_heartbeat_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint, p_writer_epoch bigint,
    p_heartbeat_ttl_seconds integer, p_transition_digest text,
    p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    heartbeat_at timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT * FROM cloud_agents.compatibility_recovery_live_instance_transition_v2(
        'heartbeat', p_tenant_id, p_service_kind, p_instance_id, p_incarnation,
        p_rollout_generation, p_writer_epoch, NULL, NULL, NULL, NULL,
        p_heartbeat_ttl_seconds, p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_live_instance_begin_drain_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint,
    p_expected_writer_epoch bigint, p_new_writer_epoch bigint,
    p_transition_digest text, p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    heartbeat_at timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT * FROM cloud_agents.compatibility_recovery_live_instance_transition_v2(
        'begin_drain', p_tenant_id, p_service_kind, p_instance_id, p_incarnation,
        p_rollout_generation, p_expected_writer_epoch, p_new_writer_epoch,
        NULL, NULL, NULL, NULL, p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_live_instance_finish_drain_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint,
    p_expected_writer_epoch bigint, p_new_writer_epoch bigint,
    p_transition_digest text, p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    heartbeat_at timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT * FROM cloud_agents.compatibility_recovery_live_instance_transition_v2(
        'finish_drain', p_tenant_id, p_service_kind, p_instance_id, p_incarnation,
        p_rollout_generation, p_expected_writer_epoch, p_new_writer_epoch,
        NULL, NULL, NULL, NULL, p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_live_instance_fence_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint,
    p_expected_writer_epoch bigint, p_new_writer_epoch bigint,
    p_transition_digest text, p_request_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    heartbeat_at timestamptz
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT * FROM cloud_agents.compatibility_recovery_live_instance_transition_v2(
        'fence', p_tenant_id, p_service_kind, p_instance_id, p_incarnation,
        p_rollout_generation, p_expected_writer_epoch, p_new_writer_epoch,
        NULL, NULL, NULL, NULL, p_transition_digest, p_request_digest
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_live_instance_reconcile_v2(
    p_tenant_id text, p_service_kind text, p_instance_id text,
    p_incarnation bigint, p_rollout_generation bigint,
    p_transition_digest text
)
RETURNS TABLE (
    result_code text, write_applied boolean, reconcile_required boolean,
    state text, version bigint, writer_epoch bigint,
    database_timestamp timestamptz, stable_error_code text,
    heartbeat_at timestamptz, heartbeat_ttl_seconds integer,
    transition_observed boolean
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    observed record;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_runtime'
    );
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_service_kind)
        OR NOT cloud_agents.is_valid_identifier(p_instance_id)
        OR p_incarnation < 1
        OR p_rollout_generation < 1
        OR p_transition_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'live-instance reconcile input is invalid';
    END IF;
    SELECT instance.*
    INTO observed
    FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
    WHERE instance.tenant_id = p_tenant_id
        AND instance.service_kind = p_service_kind
        AND instance.instance_id = p_instance_id
        AND instance.incarnation = p_incarnation
        AND instance.rollout_generation = p_rollout_generation;
    IF FOUND THEN
        result_code := 'observed';
        state := observed.drain_state;
        version := observed.version;
        writer_epoch := observed.writer_epoch;
        heartbeat_at := observed.heartbeat_at;
        heartbeat_ttl_seconds := observed.heartbeat_ttl_seconds;
    ELSE
        result_code := 'not_observed';
        state := 'absent';
        version := 0;
        writer_epoch := 0;
        heartbeat_at := NULL;
        heartbeat_ttl_seconds := NULL;
    END IF;
    SELECT EXISTS (
        SELECT 1
        FROM cloud_agents.compatibility_recovery_transition_facts_v2 AS fact
        WHERE fact.tenant_id = p_tenant_id
            AND fact.transition_digest = p_transition_digest
            AND fact.profile_id = 'live-instance/v2'
            AND fact.identity_digest =
                cloud_agents.compatibility_recovery_identity_digest_v2(
                    'live-instance/v2', p_tenant_id, p_service_kind,
                    p_instance_id, p_incarnation::text,
                    p_rollout_generation::text
                )
    ) INTO transition_observed;
    write_applied := false;
    reconcile_required := false;
    database_timestamp := pg_catalog.transaction_timestamp();
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2(
    p_tenant_id text,
    p_postgres_major integer,
    p_ledger_checksum text,
    p_target_schema_bundle_digest text,
    p_target_phase text,
    p_rollout_generation bigint,
    p_writer_epoch bigint,
    p_restore_evidence_digest text,
    p_requires_irreversible_contract_approval boolean,
    p_irreversible_contract_approval_digest text
)
RETURNS TABLE (
    result_code text,
    write_applied boolean,
    reconcile_required boolean,
    state text,
    version bigint,
    writer_epoch bigint,
    database_timestamp timestamptz,
    stable_error_code text,
    decision text,
    evaluated_at timestamptz,
    ledger_checksum text,
    postgres_major integer,
    restore_evidence_digest text,
    rollout_generation bigint,
    target_phase text,
    target_schema_bundle_digest text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    expected_profile_id constant text := 'migration-preflight/v2';
    expected_profile_digest constant text := 'sha256:e02302ea60eca9855d362d8bcab7efc0466adab6d3a486d828adccdbc5411d7a';
    actor_principal text;
    matching_restore_count bigint;
    current_writer_count bigint;
    incompatible_unexpired_count bigint;
    unretired_expired_count bigint;
BEGIN
    actor_principal := cloud_agents.compatibility_recovery_require_principal_v2(
        'cloud_agents_runtime'
    );
    evaluated_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_postgres_major NOT BETWEEN 15 AND 17
        OR p_ledger_checksum !~ '^sha256:[0-9a-f]{64}$'
        OR p_target_schema_bundle_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_target_phase !~ '^[0-9]{6}$'
        OR p_rollout_generation < 1
        OR p_writer_epoch < 1
        OR p_restore_evidence_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_requires_irreversible_contract_approval IS NULL
        OR (p_requires_irreversible_contract_approval
            AND p_irreversible_contract_approval_digest
                !~ '^sha256:[0-9a-f]{64}$')
        OR (NOT p_requires_irreversible_contract_approval
            AND p_irreversible_contract_approval_digest IS NOT NULL)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023',
            MESSAGE = 'migration preflight input is invalid';
    END IF;

    result_code := 'observed';
    write_applied := false;
    reconcile_required := false;
    state := 'rejected';
    decision := 'rejected';
    version := 0;
    writer_epoch := p_writer_epoch;
    database_timestamp := evaluated_at;
    stable_error_code := NULL;
    ledger_checksum := p_ledger_checksum;
    postgres_major := p_postgres_major;
    restore_evidence_digest := p_restore_evidence_digest;
    rollout_generation := p_rollout_generation;
    target_phase := p_target_phase;
    target_schema_bundle_digest := p_target_schema_bundle_digest;

    IF NOT cloud_agents.compatibility_recovery_profile_is_registered_v2(
            expected_profile_id, expected_profile_digest)
        OR cloud_agents.compatibility_recovery_registry_digest_v2()
            IS DISTINCT FROM 'sha256:d5ca128f28d637349dd6f8515f9ca6bb182fb0778a3160e24d731712589f2973'
        OR cloud_agents.compatibility_recovery_schema_head_v2()
            IS DISTINCT FROM '000010'
    THEN
        stable_error_code := 'preflight_registry_unavailable';
        RETURN NEXT;
        RETURN;
    END IF;

    IF p_postgres_major IS DISTINCT FROM
        (pg_catalog.current_setting('server_version_num')::integer / 10000)
    THEN
        stable_error_code := 'preflight_postgres_major_mismatch';
        RETURN NEXT;
        RETURN;
    END IF;

    IF p_requires_irreversible_contract_approval THEN
        -- A2.4 intentionally does not add an approval authority. A supplied
        -- digest cannot self-prove an irreversible contract approval.
        stable_error_code := 'preflight_irreversible_approval_unverified';
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT pg_catalog.count(*)
    INTO matching_restore_count
    FROM cloud_agents.compatibility_recovery_restore_evidence_v2 AS evidence
    WHERE evidence.tenant_id = p_tenant_id
        AND evidence.state = 'complete'
        AND evidence.scope = 'local_logical_backup_restore_and_preflight'
        AND evidence.postgres_major = p_postgres_major
        AND evidence.ledger_checksum = p_ledger_checksum
        AND evidence.target_schema_bundle_digest = p_target_schema_bundle_digest
        AND evidence.target_phase = p_target_phase
        AND evidence.evidence_digest = p_restore_evidence_digest;
    IF matching_restore_count <> 1 THEN
        stable_error_code := 'preflight_restore_evidence_mismatch';
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT pg_catalog.count(*)
    INTO current_writer_count
    FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
    WHERE instance.tenant_id = p_tenant_id
        AND instance.rollout_generation = p_rollout_generation
        AND instance.writer_epoch = p_writer_epoch
        AND instance.drain_state = 'active'
        AND instance.heartbeat_at
            + instance.heartbeat_ttl_seconds * interval '1 second'
            >= evaluated_at
        AND instance.supported_schema_min <= p_target_phase
        AND instance.supported_schema_max >= p_target_phase;
    IF current_writer_count <> 1 THEN
        stable_error_code := 'preflight_current_writer_unproven';
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT pg_catalog.count(*)
    INTO incompatible_unexpired_count
    FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
    WHERE instance.tenant_id = p_tenant_id
        AND instance.drain_state <> 'fenced'
        AND instance.heartbeat_at
            + instance.heartbeat_ttl_seconds * interval '1 second'
            >= evaluated_at
        AND NOT (
            instance.supported_schema_min <= p_target_phase
            AND instance.supported_schema_max >= p_target_phase
        );
    IF incompatible_unexpired_count <> 0 THEN
        stable_error_code := 'preflight_unexpired_instance_incompatible';
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT pg_catalog.count(*)
    INTO unretired_expired_count
    FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
    WHERE instance.tenant_id = p_tenant_id
        AND instance.drain_state <> 'fenced'
        AND instance.heartbeat_at
            + instance.heartbeat_ttl_seconds * interval '1 second'
            < evaluated_at
        AND NOT EXISTS (
            SELECT 1
            FROM cloud_agents.compatibility_recovery_retirement_receipts_v2 AS receipt
            WHERE receipt.tenant_id = instance.tenant_id
                AND receipt.service_kind = instance.service_kind
                AND receipt.instance_id = instance.instance_id
                AND receipt.incarnation = instance.incarnation
                AND receipt.rollout_generation = instance.rollout_generation
                AND receipt.writer_epoch = instance.writer_epoch
                AND receipt.state = 'complete'
                AND receipt.credential_revoked
                AND receipt.endpoint_revoked
                AND receipt.process_terminated
                AND receipt.leader_released
                AND receipt.claim_released
                AND receipt.generation_fenced
                AND receipt.receipt_digest IS NOT NULL
        );
    IF unretired_expired_count <> 0 THEN
        stable_error_code := 'preflight_expired_instance_unretired';
        RETURN NEXT;
        RETURN;
    END IF;

    state := 'approved';
    decision := 'approved';
    stable_error_code := NULL;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE INDEX compatibility_recovery_workload_principals_v2_state_idx
    ON cloud_agents.compatibility_recovery_workload_principals_v2
    (tenant_id, state, updated_at);
CREATE INDEX compatibility_recovery_backfills_v2_state_idx
    ON cloud_agents.compatibility_recovery_backfills_v2
    (tenant_id, state, updated_at);
CREATE INDEX compatibility_recovery_restore_evidence_v2_target_idx
    ON cloud_agents.compatibility_recovery_restore_evidence_v2
    (tenant_id, target_schema_bundle_digest, target_phase, state);
CREATE INDEX compatibility_recovery_live_instances_v2_preflight_idx
    ON cloud_agents.compatibility_recovery_live_instances_v2
    (tenant_id, drain_state, heartbeat_at, supported_schema_min,
        supported_schema_max);
CREATE INDEX compatibility_recovery_live_instances_v2_writer_idx
    ON cloud_agents.compatibility_recovery_live_instances_v2
    (tenant_id, rollout_generation, writer_epoch, drain_state);
CREATE INDEX compatibility_recovery_retirement_receipts_v2_state_idx
    ON cloud_agents.compatibility_recovery_retirement_receipts_v2
    (tenant_id, state, updated_at);
CREATE INDEX compatibility_recovery_transition_facts_v2_identity_idx
    ON cloud_agents.compatibility_recovery_transition_facts_v2
    (tenant_id, profile_id, identity_digest, database_timestamp);

ALTER TABLE cloud_agents.compatibility_recovery_workload_principals_v2
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_workload_principals_v2
    FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_backfills_v2
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_backfills_v2
    FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_restore_evidence_v2
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_restore_evidence_v2
    FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_live_instances_v2
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_live_instances_v2
    FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_retirement_receipts_v2
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_retirement_receipts_v2
    FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_transition_facts_v2
    ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.compatibility_recovery_transition_facts_v2
    FORCE ROW LEVEL SECURITY;

CREATE POLICY compatibility_recovery_workload_principals_v2_tenant
    ON cloud_agents.compatibility_recovery_workload_principals_v2
    TO cloud_agents_migration_owner
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY compatibility_recovery_backfills_v2_tenant
    ON cloud_agents.compatibility_recovery_backfills_v2
    TO cloud_agents_migration_owner
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY compatibility_recovery_restore_evidence_v2_tenant
    ON cloud_agents.compatibility_recovery_restore_evidence_v2
    TO cloud_agents_migration_owner
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY compatibility_recovery_live_instances_v2_tenant
    ON cloud_agents.compatibility_recovery_live_instances_v2
    TO cloud_agents_migration_owner
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY compatibility_recovery_retirement_receipts_v2_tenant
    ON cloud_agents.compatibility_recovery_retirement_receipts_v2
    TO cloud_agents_migration_owner
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());
CREATE POLICY compatibility_recovery_transition_facts_v2_tenant
    ON cloud_agents.compatibility_recovery_transition_facts_v2
    TO cloud_agents_migration_owner
    USING (tenant_id = cloud_agents.require_tenant_id())
    WITH CHECK (tenant_id = cloud_agents.require_tenant_id());

REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_workload_principals_v2
    FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_workload_principals_v2
    FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_workload_principals_v2
    FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_backfills_v2 FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_backfills_v2
    FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_backfills_v2
    FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_restore_evidence_v2 FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_restore_evidence_v2
    FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_restore_evidence_v2
    FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_live_instances_v2 FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_live_instances_v2
    FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_live_instances_v2
    FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_retirement_receipts_v2 FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_retirement_receipts_v2
    FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_retirement_receipts_v2
    FROM cloud_agents_bootstrap_admin;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_transition_facts_v2 FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_transition_facts_v2
    FROM cloud_agents_runtime;
REVOKE ALL ON TABLE cloud_agents.compatibility_recovery_transition_facts_v2
    FROM cloud_agents_bootstrap_admin;

REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_backfill_acquire_lease_v2(
    text, text, text, bigint, bigint, integer, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_backfill_advance_v2(
    text, text, text, bigint, text, text, text, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_backfill_complete_v2(
    text, text, text, bigint, text, text, text, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_backfill_heartbeat_v2(
    text, text, text, bigint, integer, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_backfill_reconcile_v2(
    text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_backfill_start_v2(
    text, text, text, text, text, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_backfill_transition_v2(
    text, text, text, text, text, text, bigint, text, bigint, bigint, integer, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_identity_digest_v2(
    text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_live_instance_activate_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_live_instance_begin_drain_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_live_instance_fence_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_live_instance_finish_drain_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_live_instance_heartbeat_v2(
    text, text, text, bigint, bigint, bigint, integer, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_live_instance_reconcile_v2(
    text, text, text, bigint, bigint, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_live_instance_register_v2(
    text, text, text, bigint, bigint, bigint, text, text, text, integer, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_live_instance_transition_v2(
    text, text, text, text, bigint, bigint, bigint, bigint, text, text, text, integer,
    text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_lock_v2(text, text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2(
    text, integer, text, text, text, bigint, bigint, text, boolean, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_policy_digest_v2() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_profile_digest_v2(text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_profile_is_registered_v2(
    text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_record_transition_v2(
    text, text, text, text, text, text, text, text, text, bigint, bigint, text, timestamptz
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_registry_digest_v2() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_require_principal_v2(text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_restore_evidence_complete_v2(
    text, text, bigint, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_restore_evidence_reconcile_v2(
    text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_restore_evidence_record_v2(
    text, text, integer, text, text, text, text, text, timestamptz, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_restore_evidence_reject_v2(
    text, text, bigint, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_restore_evidence_transition_v2(
    text, text, text, bigint, integer, text, text, text, text, text, timestamptz, text,
    text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_collect_v2(
    text, text, text, bigint, bigint, bigint, bigint, boolean, boolean, boolean, boolean,
    boolean, boolean, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_complete_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_reconcile_v2(
    text, text, text, bigint, bigint, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_reject_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_transition_v2(
    text, text, text, text, bigint, bigint, bigint, bigint, boolean, boolean, boolean,
    boolean, boolean, boolean, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_schema_head_v2() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_schema_migration_digest_v2()
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_state_machine_digest_v2()
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_transition_lock_v2(text, text)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_reconcile_v2(
    text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_register_v2(
    text, text, text, text, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_revoke_v2(
    text, text, text, text, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_rotate_v2(
    text, text, text, text, text, bigint, bigint, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_transition_v2(
    text, text, text, text, text, text, bigint, bigint, text, text
) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_registry_digest_v2()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_state_machine_digest_v2()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_policy_digest_v2()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_schema_head_v2()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_schema_catalog_digest_v2()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_schema_migration_digest_v2()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_profile_digest_v2(text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_profile_is_registered_v2(
    text, text
) TO cloud_agents_runtime;

GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_register_v2(
    text, text, text, text, bigint, text, text
) TO cloud_agents_bootstrap_admin;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_rotate_v2(
    text, text, text, text, text, bigint, bigint, text, text
) TO cloud_agents_bootstrap_admin;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_revoke_v2(
    text, text, text, text, bigint, text, text
) TO cloud_agents_bootstrap_admin;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_workload_principal_reconcile_v2(
    text, text, text, text
) TO cloud_agents_bootstrap_admin;

GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_live_instance_register_v2(
    text, text, text, bigint, bigint, bigint, text, text, text, integer,
    text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_live_instance_activate_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_live_instance_heartbeat_v2(
    text, text, text, bigint, bigint, bigint, integer, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_live_instance_begin_drain_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_live_instance_finish_drain_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_live_instance_fence_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_live_instance_reconcile_v2(
    text, text, text, bigint, bigint, text
) TO cloud_agents_runtime;

GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_collect_v2(
    text, text, text, bigint, bigint, bigint, bigint, boolean, boolean,
    boolean, boolean, boolean, boolean, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_complete_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_reject_v2(
    text, text, text, bigint, bigint, bigint, bigint, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_retirement_receipt_reconcile_v2(
    text, text, text, bigint, bigint, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2(
    text, integer, text, text, text, bigint, bigint, text, boolean, text
) TO cloud_agents_runtime;
