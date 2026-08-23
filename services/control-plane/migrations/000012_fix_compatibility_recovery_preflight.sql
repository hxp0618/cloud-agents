-- Append-only repair for the compatibility recovery migration preflight.
-- A fenced live-instance row is not, by itself, proof that the process,
-- endpoint, credential, claim, and leader have all been retired. Only the
-- exact complete retirement receipt may remove an incompatible or expired
-- instance from the preflight live set.

CREATE OR REPLACE FUNCTION cloud_agents.compatibility_recovery_migration_preflight_evaluate_v2(
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
        AND instance.heartbeat_at
            + instance.heartbeat_ttl_seconds * interval '1 second'
            >= evaluated_at
        AND NOT (
            instance.supported_schema_min <= p_target_phase
            AND instance.supported_schema_max >= p_target_phase
        )
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
    IF incompatible_unexpired_count <> 0 THEN
        stable_error_code := 'preflight_unexpired_instance_incompatible';
        RETURN NEXT;
        RETURN;
    END IF;

    SELECT pg_catalog.count(*)
    INTO unretired_expired_count
    FROM cloud_agents.compatibility_recovery_live_instances_v2 AS instance
    WHERE instance.tenant_id = p_tenant_id
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
