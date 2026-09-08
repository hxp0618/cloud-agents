
ALTER TABLE cloud_agents.workspace_snapshots
    ADD COLUMN retention_seconds integer,
    ADD COLUMN expires_at timestamptz,
    ADD COLUMN cleanup_operation_id text,
    ADD COLUMN cleanup_trigger text,
    ADD COLUMN deleted_at timestamptz;
ALTER TABLE cloud_agents.workspace_snapshots
    ADD CONSTRAINT workspace_snapshots_retention CHECK (
        retention_seconds IS NULL AND expires_at IS NULL
        OR retention_seconds BETWEEN 1 AND 31536000 AND expires_at IS NOT NULL
    ),
    ADD CONSTRAINT workspace_snapshots_cleanup_operation CHECK (
        cleanup_operation_id IS NULL OR cloud_agents.is_valid_identifier(cleanup_operation_id)
    ),
    ADD CONSTRAINT workspace_snapshots_cleanup_trigger CHECK (
        cleanup_trigger IS NULL OR cleanup_trigger IN ('manual', 'retention')
    );
ALTER TABLE cloud_agents.workspace_snapshots DROP CONSTRAINT workspace_snapshots_status_check;
ALTER TABLE cloud_agents.workspace_snapshots DROP CONSTRAINT workspace_snapshots_check;
ALTER TABLE cloud_agents.workspace_snapshots
    ADD CONSTRAINT workspace_snapshots_status_check
        CHECK (status IN ('pending', 'available', 'unknown', 'failed', 'deleting', 'cleanup_failed', 'deleted')),
    ADD CONSTRAINT workspace_snapshots_check CHECK (
        status = 'pending' AND physical_snapshot_uid IS NULL AND content_digest IS NULL
            AND size_bytes IS NULL AND stable_error_code IS NULL AND observed_at IS NULL
            AND cleanup_operation_id IS NULL AND cleanup_trigger IS NULL AND deleted_at IS NULL
        OR status = 'available' AND physical_snapshot_uid IS NOT NULL AND content_digest IS NOT NULL
            AND size_bytes IS NOT NULL AND stable_error_code IS NULL AND observed_at IS NOT NULL
            AND cleanup_operation_id IS NULL AND cleanup_trigger IS NULL AND deleted_at IS NULL
        OR status = 'unknown' AND physical_snapshot_uid IS NULL AND content_digest IS NULL
            AND size_bytes IS NULL AND stable_error_code IS NULL AND observed_at IS NOT NULL
            AND cleanup_operation_id IS NULL AND cleanup_trigger IS NULL AND deleted_at IS NULL
        OR status = 'failed' AND physical_snapshot_uid IS NULL AND content_digest IS NULL
            AND size_bytes IS NULL AND stable_error_code IS NOT NULL AND observed_at IS NOT NULL
            AND cleanup_operation_id IS NULL AND cleanup_trigger IS NULL AND deleted_at IS NULL
        OR status = 'deleting' AND physical_snapshot_uid IS NOT NULL AND content_digest IS NOT NULL
            AND size_bytes IS NOT NULL AND stable_error_code IS NULL AND observed_at IS NOT NULL
            AND cleanup_operation_id IS NOT NULL AND cleanup_trigger IS NOT NULL AND deleted_at IS NULL
        OR status = 'cleanup_failed' AND physical_snapshot_uid IS NOT NULL AND content_digest IS NOT NULL
            AND size_bytes IS NOT NULL AND stable_error_code IS NOT NULL AND observed_at IS NOT NULL
            AND cleanup_operation_id IS NOT NULL AND cleanup_trigger IS NOT NULL AND deleted_at IS NULL
        OR status = 'deleted' AND physical_snapshot_uid IS NULL AND content_digest IS NOT NULL
            AND size_bytes IS NOT NULL AND stable_error_code IS NULL AND observed_at IS NOT NULL
            AND cleanup_operation_id IS NOT NULL AND cleanup_trigger IS NOT NULL AND deleted_at IS NOT NULL
    );
CREATE INDEX workspace_snapshots_expiry_idx ON cloud_agents.workspace_snapshots
    (expires_at, tenant_id, project_uid, snapshot_uid);

CREATE FUNCTION cloud_agents.accept_foundation_workspace_snapshot_v2(
    p_project text, p_snapshot text, p_sandbox text, p_expected_sandbox_generation bigint,
    p_retention_seconds integer, p_subject text, p_key text, p_digest text
)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE operation_value text; accepted_at timestamptz; expiry timestamptz;
BEGIN
    IF p_retention_seconds NOT BETWEEN 1 AND 31536000 THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot retention is invalid';
    END IF;
    operation_value := cloud_agents.accept_foundation_workspace_snapshot_v1(
        p_project, p_snapshot, p_sandbox, p_expected_sandbox_generation, p_subject, p_key, p_digest
    );
    SELECT operation.created_at INTO STRICT accepted_at FROM cloud_agents.platform_operations AS operation
    WHERE operation.tenant_id = cloud_agents.require_tenant_id()
      AND operation.operation_id = operation_value AND operation.operation_generation = 1;
    expiry := accepted_at + pg_catalog.make_interval(secs => p_retention_seconds);
    UPDATE cloud_agents.workspace_snapshots AS snapshot
    SET retention_seconds = p_retention_seconds, expires_at = expiry
    WHERE snapshot.tenant_id = cloud_agents.require_tenant_id()
      AND snapshot.project_uid = p_project AND snapshot.snapshot_uid = p_snapshot
      AND (snapshot.retention_seconds IS NULL AND snapshot.expires_at IS NULL
        OR snapshot.retention_seconds = p_retention_seconds AND snapshot.expires_at = expiry);
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot retention conflict'; END IF;
    RETURN operation_value;
END;
$$;

CREATE FUNCTION cloud_agents.reject_expired_workspace_snapshot_restore_v1()
RETURNS trigger LANGUAGE plpgsql SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM cloud_agents.workspace_snapshots AS snapshot
        WHERE snapshot.tenant_id = NEW.tenant_id AND snapshot.project_uid = NEW.project_uid
          AND snapshot.snapshot_uid = NEW.snapshot_uid AND snapshot.expires_at IS NOT NULL
          AND snapshot.expires_at <= pg_catalog.clock_timestamp()
    ) THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot restore conflict'; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER workspace_snapshot_restores_reject_expired
    BEFORE INSERT ON cloud_agents.workspace_snapshot_restores
    FOR EACH ROW EXECUTE FUNCTION cloud_agents.reject_expired_workspace_snapshot_restore_v1();

CREATE FUNCTION cloud_agents.accept_foundation_workspace_snapshot_cleanup_v1(
    p_project text, p_snapshot text, p_expected_snapshot_version bigint,
    p_confirmed_snapshot text, p_confirmed_workspace text, p_disposition text,
    p_trigger text, p_subject text, p_key text, p_digest text
)
RETURNS text LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    actor text;
    tenant text;
    accepted_at timestamptz := transaction_timestamp();
    selected_snapshot cloud_agents.workspace_snapshots%ROWTYPE;
    operation_value text;
    event_value text;
    audit_value text;
    prior_digest text;
    prior_operation text;
    expected_revision bigint;
    next_revision bigint;
BEGIN
    actor := cloud_agents.require_runtime_mutation_principal();
    tenant := cloud_agents.require_tenant_id();
    IF NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_snapshot)
      OR p_expected_snapshot_version < 1 OR p_confirmed_snapshot IS DISTINCT FROM p_snapshot
      OR NOT cloud_agents.is_valid_identifier(p_confirmed_workspace) OR p_disposition <> 'delete'
      OR p_trigger NOT IN ('manual', 'retention') OR p_subject !~ '^sha256:[0-9a-f]{64}$'
      OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot cleanup request is invalid'; END IF;

    SELECT record.request_digest, record.operation_id INTO prior_digest, prior_operation
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = tenant AND record.subject_digest = p_subject
      AND record.profile_id = '@PROFILE_ID@' AND record.profile_digest = '@PROFILE_DIGEST@'
      AND record.idempotency_key = p_key FOR UPDATE;
    IF FOUND THEN
        IF prior_digest IS DISTINCT FROM p_digest OR NOT EXISTS (
            SELECT 1 FROM cloud_agents.workspace_snapshots AS snapshot
            WHERE snapshot.tenant_id = tenant AND snapshot.project_uid = p_project
              AND snapshot.snapshot_uid = p_snapshot AND snapshot.source_workspace_uid = p_confirmed_workspace
              AND snapshot.cleanup_operation_id = prior_operation
        ) THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot cleanup idempotency conflict'; END IF;
        RETURN prior_operation;
    END IF;

    SELECT snapshot.* INTO selected_snapshot FROM cloud_agents.workspace_snapshots AS snapshot
    WHERE snapshot.tenant_id = tenant AND snapshot.project_uid = p_project AND snapshot.snapshot_uid = p_snapshot
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'workspace snapshot was not found'; END IF;
    IF selected_snapshot.resource_version <> p_expected_snapshot_version
      OR selected_snapshot.source_workspace_uid <> p_confirmed_workspace
      OR selected_snapshot.status NOT IN ('available', 'cleanup_failed')
      OR selected_snapshot.physical_snapshot_uid IS NULL OR selected_snapshot.content_digest IS NULL
      OR selected_snapshot.size_bytes IS NULL
      OR EXISTS (
        SELECT 1 FROM cloud_agents.workspace_snapshot_restores AS restore
        JOIN cloud_agents.platform_operations AS operation
          ON operation.tenant_id = restore.tenant_id AND operation.operation_id = restore.operation_id
         AND operation.operation_generation = restore.operation_generation
        WHERE restore.tenant_id = tenant AND restore.project_uid = p_project AND restore.snapshot_uid = p_snapshot
          AND operation.state IN ('pending', 'running', 'reconciling')
      )
    THEN RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'workspace snapshot cleanup conflict'; END IF;

    operation_value := 'op-' || pg_catalog.md5(tenant || '|' || p_project || '|' || p_snapshot || '|cleanup|' || p_key);
    event_value := 'evt-' || pg_catalog.md5(tenant || '|' || operation_value || '|cleanup');
    audit_value := 'audit-' || pg_catalog.md5(tenant || '|' || operation_value || '|accept');
    INSERT INTO cloud_agents.idempotency_records
        (tenant_id, tenant_ref_id, subject_digest, registry_digest, profile_id, profile_digest,
         idempotency_key, request_digest, state, expires_at)
    VALUES (tenant, tenant, p_subject, '@REGISTRY_DIGEST@', '@PROFILE_ID@', '@PROFILE_DIGEST@',
        p_key, p_digest, 'pending', transaction_timestamp() + interval '1 day');
    INSERT INTO cloud_agents.platform_operations
        (tenant_id, tenant_ref_id, operation_id, operation_generation, registry_digest, state_machine_digest,
         policy_digest, profile_id, profile_digest, subject_digest, request_digest, state, cleanup_phase,
         recovery_generation, current_attempt_number)
    VALUES (tenant, tenant, operation_value, 1, '@REGISTRY_DIGEST@', cloud_agents.coordination_state_machine_digest(),
        cloud_agents.coordination_policy_digest(), '@PROFILE_ID@', '@PROFILE_DIGEST@', p_subject, p_digest,
        'pending', 'none', 0, 0);
    INSERT INTO cloud_agents.operation_finalizers
        (tenant_id, tenant_ref_id, operation_id, operation_generation, finalizer_name, required, state, delivery_attempts)
    VALUES (tenant, tenant, operation_value, 1, 'workspace-snapshot-delete', true, 'pending', 0);
    INSERT INTO cloud_agents.outbox_events
        (tenant_id, tenant_ref_id, event_id, registry_digest, profile_id, profile_digest, event_class,
         aggregate_kind, aggregate_id, aggregate_sequence, generation, operation_id, operation_generation,
         payload_digest, state, delivery_attempts)
    VALUES (tenant, tenant, event_value, '@REGISTRY_DIGEST@', '@PROFILE_ID@', '@PROFILE_DIGEST@', 'operation_effect',
        'workspaceSnapshot', p_snapshot, selected_snapshot.resource_version + 1, 1, operation_value, 1, p_digest, 'pending', 0);
    INSERT INTO cloud_agents.coordination_audit_facts
        (tenant_id, tenant_ref_id, audit_fact_id, registry_digest, profile_id, profile_digest,
         subject_digest, operation_id, operation_generation, transition, outcome)
    VALUES (tenant, tenant, audit_value, '@REGISTRY_DIGEST@', '@PROFILE_ID@', '@PROFILE_DIGEST@',
        p_subject, operation_value, 1, 'workspace.snapshot.cleanup.accept.' || p_trigger, 'pending');
    SELECT revision.current_revision INTO expected_revision FROM cloud_agents.tenant_resource_versions AS revision
    WHERE revision.tenant_id = tenant AND revision.tenant_uid = tenant FOR UPDATE;
    next_revision := cloud_agents.allocate_tenant_revision(tenant, expected_revision, accepted_at);
    INSERT INTO cloud_agents.resource_changes
        (tenant_id, tenant_uid, resource_version, resource_kind, resource_uid, change_kind, actor_database_principal, occurred_at)
    VALUES (tenant, tenant, next_revision, 'workspaceSnapshot', p_snapshot, 'updated', actor, accepted_at);
    UPDATE cloud_agents.workspace_snapshots SET status = 'deleting', cleanup_operation_id = operation_value,
        cleanup_trigger = p_trigger, stable_error_code = NULL, resource_version = next_revision,
        updated_at = accepted_at, deleted_at = NULL
    WHERE tenant_id = tenant AND project_uid = p_project AND snapshot_uid = p_snapshot;
    UPDATE cloud_agents.idempotency_records SET operation_id = operation_value, operation_generation = 1
    WHERE tenant_id = tenant AND subject_digest = p_subject AND profile_id = '@PROFILE_ID@'
      AND profile_digest = '@PROFILE_DIGEST@' AND idempotency_key = p_key;
    RETURN operation_value;
END;
$$;

CREATE FUNCTION cloud_agents.expire_foundation_workspace_snapshot_v1(p_subject text)
RETURNS TABLE (tenant_id text, project_uid text, snapshot_uid text, operation_uid text, expired_at timestamptz)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE candidate record; key_value text; digest_value text;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_subject !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot expiry request is invalid';
    END IF;
    SELECT snapshot.tenant_id, snapshot.project_uid, snapshot.snapshot_uid,
      snapshot.source_workspace_uid, snapshot.resource_version, snapshot.expires_at
    INTO candidate FROM cloud_agents.workspace_snapshots AS snapshot
    WHERE snapshot.status = 'available' AND snapshot.expires_at IS NOT NULL
      AND snapshot.expires_at <= pg_catalog.clock_timestamp()
      AND NOT EXISTS (
        SELECT 1 FROM cloud_agents.workspace_snapshot_restores AS restore
        JOIN cloud_agents.platform_operations AS operation
          ON operation.tenant_id = restore.tenant_id AND operation.operation_id = restore.operation_id
         AND operation.operation_generation = restore.operation_generation
        WHERE restore.tenant_id = snapshot.tenant_id AND restore.project_uid = snapshot.project_uid
          AND restore.snapshot_uid = snapshot.snapshot_uid
          AND operation.state IN ('pending', 'running', 'reconciling')
      )
    ORDER BY snapshot.expires_at, snapshot.tenant_id, snapshot.project_uid, snapshot.snapshot_uid
    FOR UPDATE SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);
    key_value := 'snapshot-retention-' || pg_catalog.md5(candidate.tenant_id || '|' || candidate.project_uid
      || '|' || candidate.snapshot_uid || '|' || candidate.expires_at::text);
    digest_value := 'sha256:' || pg_catalog.encode(pg_catalog.sha256(pg_catalog.convert_to(
      candidate.tenant_id || '|' || candidate.project_uid || '|' || candidate.snapshot_uid || '|'
      || candidate.source_workspace_uid || '|' || candidate.resource_version::text || '|retention', 'UTF8')), 'hex');
    operation_uid := cloud_agents.accept_foundation_workspace_snapshot_cleanup_v1(
      candidate.project_uid, candidate.snapshot_uid, candidate.resource_version, candidate.snapshot_uid,
      candidate.source_workspace_uid, 'delete', 'retention', p_subject, key_value, digest_value);
    tenant_id := candidate.tenant_id; project_uid := candidate.project_uid;
    snapshot_uid := candidate.snapshot_uid; expired_at := candidate.expires_at;
    RETURN NEXT;
END;
$$;

CREATE FUNCTION cloud_agents.claim_foundation_workspace_snapshot_cleanup_v1(
    p_holder text, p_incarnation text, p_token text, p_lease_seconds integer, p_subject text, p_audit text
)
RETURNS TABLE (
    tenant_id text, event_id text, delivery_attempts integer, claim_expires_at timestamptz,
    operation_id text, project_uid text, snapshot_uid text, source_workspace_uid text,
    target_uid text, target_endpoint text, credential_ref text, physical_snapshot_uid text
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE claimed_at timestamptz := transaction_timestamp(); candidate cloud_agents.outbox_events%ROWTYPE;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF NOT cloud_agents.is_valid_identifier(p_holder) OR NOT cloud_agents.is_valid_identifier(p_incarnation)
      OR NOT cloud_agents.is_valid_identifier(p_token) OR p_lease_seconds NOT BETWEEN 1 AND 60
      OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot cleanup claim is invalid'; END IF;
    SELECT event.* INTO candidate FROM cloud_agents.outbox_events AS event
    JOIN cloud_agents.workspace_snapshots AS snapshot
      ON snapshot.tenant_id = event.tenant_id AND snapshot.cleanup_operation_id = event.operation_id
     AND snapshot.snapshot_uid = event.aggregate_id
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = snapshot.tenant_id AND target.project_uid = snapshot.project_uid
     AND target.target_uid = snapshot.target_uid
    WHERE event.profile_id = '@PROFILE_ID@' AND event.profile_digest = '@PROFILE_DIGEST@'
      AND event.event_class = 'operation_effect' AND event.aggregate_kind = 'workspaceSnapshot'
      AND (event.state = 'pending' OR event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at)
      AND event.delivery_attempts < 8 AND snapshot.status = 'deleting'
      AND snapshot.physical_snapshot_uid IS NOT NULL AND target.target_kind = 'docker'
    ORDER BY event.created_at, event.tenant_id, event.event_id FOR UPDATE OF event SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);
    UPDATE cloud_agents.outbox_events AS event SET state = 'claimed', delivery_attempts = event.delivery_attempts + 1,
      next_attempt_at = NULL, claim_holder_id = p_holder, claim_incarnation = p_incarnation, claim_token = p_token,
      claim_started_at = claimed_at, claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds), updated_at = claimed_at
    WHERE event.tenant_id = candidate.tenant_id AND event.event_id = candidate.event_id RETURNING event.* INTO candidate;
    UPDATE cloud_agents.platform_operations AS operation
    SET state = CASE WHEN candidate.delivery_attempts = 1 THEN 'running' ELSE 'reconciling' END,
      recovery_generation = candidate.delivery_attempts - 1, current_attempt_number = candidate.delivery_attempts, updated_at = claimed_at
    WHERE operation.tenant_id = candidate.tenant_id AND operation.operation_id = candidate.operation_id
      AND operation.operation_generation = candidate.operation_generation AND operation.state IN ('pending', 'running', 'reconciling');
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'workspace snapshot cleanup operation is not claimable'; END IF;
    INSERT INTO cloud_agents.operation_attempts
      (tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number, state,
       claim_holder_id, claim_incarnation, claim_token, claim_started_at, claim_expires_at, created_at, updated_at)
    VALUES (candidate.tenant_id, candidate.tenant_id, candidate.operation_id, candidate.operation_generation,
      candidate.delivery_attempts, 'claimed', p_holder, p_incarnation, p_token, claimed_at, candidate.claim_expires_at, claimed_at, claimed_at);
    PERFORM cloud_agents.append_coordination_audit(candidate.tenant_id, p_audit, candidate.profile_id, candidate.profile_digest,
      p_subject, candidate.operation_id, candidate.operation_generation, candidate.delivery_attempts,
      NULL, NULL, NULL, 'workspace.snapshot.cleanup.claim', 'pending', NULL, NULL, claimed_at);
    RETURN QUERY SELECT candidate.tenant_id, candidate.event_id, candidate.delivery_attempts, candidate.claim_expires_at,
      candidate.operation_id, snapshot.project_uid, snapshot.snapshot_uid, snapshot.source_workspace_uid,
      snapshot.target_uid, target.endpoint, target.credential_ref, snapshot.physical_snapshot_uid
    FROM cloud_agents.workspace_snapshots AS snapshot
    JOIN cloud_agents.deployment_targets AS target
      ON target.tenant_id = snapshot.tenant_id AND target.project_uid = snapshot.project_uid AND target.target_uid = snapshot.target_uid
    WHERE snapshot.tenant_id = candidate.tenant_id AND snapshot.cleanup_operation_id = candidate.operation_id;
END;
$$;

CREATE FUNCTION cloud_agents.renew_foundation_workspace_snapshot_cleanup_claim_v1(
    p_tenant text, p_event text, p_holder text, p_incarnation text, p_token text,
    p_prior_expiry timestamptz, p_lease_seconds integer
)
RETURNS timestamptz LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE renewed_at timestamptz := transaction_timestamp(); renewed_expiry timestamptz;
BEGIN
    PERFORM cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id() OR p_lease_seconds NOT BETWEEN 1 AND 60
      OR NOT cloud_agents.is_valid_identifier(p_event) OR NOT cloud_agents.is_valid_identifier(p_holder)
      OR NOT cloud_agents.is_valid_identifier(p_incarnation) OR NOT cloud_agents.is_valid_identifier(p_token)
      OR p_prior_expiry IS NULL
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot cleanup renewal is invalid'; END IF;
    renewed_expiry := renewed_at + pg_catalog.make_interval(secs => p_lease_seconds);
    UPDATE cloud_agents.outbox_events AS event SET claim_expires_at = renewed_expiry, updated_at = renewed_at
    WHERE event.tenant_id = p_tenant AND event.event_id = p_event AND event.state = 'claimed'
      AND event.profile_id = '@PROFILE_ID@' AND event.profile_digest = '@PROFILE_DIGEST@'
      AND event.claim_holder_id = p_holder AND event.claim_incarnation = p_incarnation
      AND event.claim_token = p_token AND event.claim_expires_at = p_prior_expiry AND event.claim_expires_at > renewed_at;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'workspace snapshot cleanup claim is stale'; END IF;
    UPDATE cloud_agents.operation_attempts AS attempt SET claim_expires_at = renewed_expiry, updated_at = renewed_at
    FROM cloud_agents.outbox_events AS event WHERE event.tenant_id = p_tenant AND event.event_id = p_event
      AND attempt.tenant_id = event.tenant_id AND attempt.operation_id = event.operation_id
      AND attempt.operation_generation = event.operation_generation AND attempt.attempt_number = event.delivery_attempts
      AND attempt.state = 'claimed' AND attempt.claim_holder_id = p_holder
      AND attempt.claim_incarnation = p_incarnation AND attempt.claim_token = p_token
      AND attempt.claim_expires_at = p_prior_expiry;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'workspace snapshot cleanup attempt is stale'; END IF;
    RETURN renewed_expiry;
END;
$$;

CREATE FUNCTION cloud_agents.settle_foundation_workspace_snapshot_cleanup_v1(
    p_tenant text, p_event text, p_holder text, p_incarnation text, p_token text,
    p_expiry timestamptz, p_transition text, p_error text, p_subject text, p_audit text
)
RETURNS TABLE (outbox_state text, operation_state text, resource_version bigint)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    actor text;
    settled_at timestamptz := transaction_timestamp();
    event cloud_agents.outbox_events%ROWTYPE;
    expected_revision bigint;
    next_revision bigint;
BEGIN
    actor := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id() OR p_transition NOT IN ('succeeded', 'retry', 'failed')
      OR NOT cloud_agents.is_valid_identifier(p_event) OR NOT cloud_agents.is_valid_identifier(p_holder)
      OR NOT cloud_agents.is_valid_identifier(p_incarnation) OR NOT cloud_agents.is_valid_identifier(p_token)
      OR p_expiry IS NULL OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
      OR p_transition = 'succeeded' AND p_error IS NOT NULL
      OR p_transition <> 'succeeded' AND NOT cloud_agents.is_valid_identifier(p_error)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot cleanup settlement is invalid'; END IF;
    SELECT stored.* INTO event FROM cloud_agents.outbox_events AS stored
    WHERE stored.tenant_id = p_tenant AND stored.event_id = p_event FOR UPDATE;
    IF NOT FOUND OR event.state <> 'claimed' OR event.profile_id <> '@PROFILE_ID@'
      OR event.profile_digest <> '@PROFILE_DIGEST@' OR event.claim_holder_id IS DISTINCT FROM p_holder
      OR event.claim_incarnation IS DISTINCT FROM p_incarnation OR event.claim_token IS DISTINCT FROM p_token
      OR event.claim_expires_at IS DISTINCT FROM p_expiry OR event.claim_expires_at <= settled_at
      OR p_transition = 'retry' AND event.delivery_attempts >= 8
    THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'workspace snapshot cleanup settlement claim is stale'; END IF;
    PERFORM * FROM cloud_agents.transition_outbox_claim(
      p_tenant, p_event, p_holder, p_incarnation, p_token, p_expiry,
      CASE p_transition WHEN 'succeeded' THEN 'delivery_succeeded'
        WHEN 'retry' THEN 'delivery_failed_retryable' ELSE 'delivery_failed_terminal' END,
      CASE WHEN p_transition = 'failed' THEN p_error END, p_subject, p_audit);
    UPDATE cloud_agents.operation_attempts SET state = CASE WHEN p_transition = 'succeeded' THEN 'succeeded' ELSE 'failed' END,
      claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL,
      claim_started_at = NULL, claim_expires_at = NULL, stable_error_code = p_error,
      updated_at = settled_at, terminal_at = settled_at
    WHERE tenant_id = p_tenant AND operation_id = event.operation_id
      AND operation_generation = event.operation_generation AND attempt_number = event.delivery_attempts;
    IF p_transition = 'retry' THEN
      UPDATE cloud_agents.platform_operations SET state = 'reconciling', updated_at = settled_at
      WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
      UPDATE cloud_agents.workspace_snapshots SET status = 'deleting', stable_error_code = NULL,
        updated_at = settled_at, observed_at = settled_at
      WHERE tenant_id = p_tenant AND cleanup_operation_id = event.operation_id AND snapshot_uid = event.aggregate_id;
      outbox_state := 'retry_wait'; operation_state := 'reconciling'; resource_version := NULL;
      RETURN NEXT; RETURN;
    END IF;
    SELECT revision.current_revision INTO expected_revision FROM cloud_agents.tenant_resource_versions AS revision
    WHERE revision.tenant_id = p_tenant AND revision.tenant_uid = p_tenant FOR UPDATE;
    next_revision := cloud_agents.allocate_tenant_revision(p_tenant, expected_revision, settled_at);
    INSERT INTO cloud_agents.resource_changes
      (tenant_id, tenant_uid, resource_version, resource_kind, resource_uid, change_kind, actor_database_principal, occurred_at)
    VALUES (p_tenant, p_tenant, next_revision, 'workspaceSnapshot', event.aggregate_id,
      CASE WHEN p_transition = 'succeeded' THEN 'deleted' ELSE 'updated' END, actor, settled_at);
    IF p_transition = 'succeeded' THEN
      UPDATE cloud_agents.workspace_snapshots SET status = 'deleted', physical_snapshot_uid = NULL,
        stable_error_code = NULL, resource_version = next_revision, updated_at = settled_at,
        observed_at = settled_at, deleted_at = settled_at
      WHERE tenant_id = p_tenant AND cleanup_operation_id = event.operation_id AND snapshot_uid = event.aggregate_id;
      UPDATE cloud_agents.platform_operations SET state = 'succeeded', cleanup_phase = 'complete',
        terminal_resource_kind = 'workspaceSnapshot', terminal_resource_id = event.aggregate_id,
        terminal_resource_version = next_revision, updated_at = settled_at, terminal_at = settled_at
      WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
      UPDATE cloud_agents.operation_finalizers SET state = 'succeeded', updated_at = settled_at, terminal_at = settled_at
      WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
      UPDATE cloud_agents.idempotency_records SET state = 'succeeded', resource_kind = 'workspaceSnapshot',
        resource_id = event.aggregate_id, resource_version = next_revision, updated_at = settled_at, terminal_at = settled_at
      WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
      INSERT INTO cloud_agents.terminal_receipts
        (tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number,
         receipt_id, outcome, resource_kind, resource_id, resource_version, persisted_at)
      VALUES (p_tenant, p_tenant, event.operation_id, event.operation_generation, event.delivery_attempts,
        'success', 'succeeded', 'workspaceSnapshot', event.aggregate_id, next_revision, settled_at);
      outbox_state := 'delivered'; operation_state := 'succeeded'; resource_version := next_revision;
    ELSE
      UPDATE cloud_agents.workspace_snapshots SET status = 'cleanup_failed', stable_error_code = p_error,
        resource_version = next_revision, updated_at = settled_at, observed_at = settled_at
      WHERE tenant_id = p_tenant AND cleanup_operation_id = event.operation_id AND snapshot_uid = event.aggregate_id;
      UPDATE cloud_agents.platform_operations SET state = 'failed', cleanup_phase = 'blocked', terminal_error_code = p_error,
        updated_at = settled_at, terminal_at = settled_at
      WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
      UPDATE cloud_agents.operation_finalizers SET state = 'dead_letter', stable_error_code = p_error,
        updated_at = settled_at, terminal_at = settled_at
      WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
      UPDATE cloud_agents.idempotency_records SET state = 'failed', stable_error_code = p_error,
        updated_at = settled_at, terminal_at = settled_at
      WHERE tenant_id = p_tenant AND operation_id = event.operation_id AND operation_generation = event.operation_generation;
      INSERT INTO cloud_agents.terminal_receipts
        (tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number,
         receipt_id, outcome, stable_error_code, persisted_at)
      VALUES (p_tenant, p_tenant, event.operation_id, event.operation_generation, event.delivery_attempts,
        'failure', 'failed', p_error, settled_at);
      outbox_state := 'dead_letter'; operation_state := 'failed'; resource_version := next_revision;
    END IF;
    RETURN NEXT;
END;
$$;

CREATE FUNCTION cloud_agents.reap_foundation_workspace_snapshot_cleanup_claim_v1(p_subject text, p_audit text)
RETURNS TABLE (tenant_id text, event_id text, outbox_state text, delivery_attempts integer)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE reaped_at timestamptz := transaction_timestamp(); candidate cloud_agents.outbox_events%ROWTYPE;
  error_code constant text := 'workspace_snapshot_cleanup_claim_expired'; expected_revision bigint; next_revision bigint; actor text;
BEGIN
    actor := cloud_agents.require_runtime_mutation_principal();
    IF p_subject !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_audit)
    THEN RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'workspace snapshot cleanup reaper is invalid'; END IF;
    SELECT event.* INTO candidate FROM cloud_agents.outbox_events AS event
    WHERE event.profile_id = '@PROFILE_ID@' AND event.profile_digest = '@PROFILE_DIGEST@'
      AND event.event_class = 'operation_effect' AND event.aggregate_kind = 'workspaceSnapshot'
      AND event.state = 'claimed' AND event.claim_expires_at <= reaped_at
    ORDER BY event.claim_expires_at, event.tenant_id, event.event_id FOR UPDATE SKIP LOCKED LIMIT 1;
    IF NOT FOUND THEN RETURN; END IF;
    PERFORM pg_catalog.set_config('cloud_agents.tenant_id', candidate.tenant_id, true);
    UPDATE cloud_agents.operation_attempts SET state = CASE WHEN candidate.delivery_attempts < 8 THEN 'unknown' ELSE 'failed' END,
      claim_holder_id = NULL, claim_incarnation = NULL, claim_token = NULL, claim_started_at = NULL, claim_expires_at = NULL,
      stable_error_code = CASE WHEN candidate.delivery_attempts < 8 THEN NULL ELSE error_code END,
      updated_at = reaped_at, terminal_at = CASE WHEN candidate.delivery_attempts < 8 THEN NULL ELSE reaped_at END
    WHERE tenant_id = candidate.tenant_id AND operation_id = candidate.operation_id
      AND operation_generation = candidate.operation_generation AND attempt_number = candidate.delivery_attempts AND state = 'claimed';
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'workspace snapshot cleanup attempt authority drifted'; END IF;
    IF candidate.delivery_attempts < 8 THEN
      outbox_state := 'pending';
      UPDATE cloud_agents.outbox_events SET state = 'pending', claim_holder_id = NULL, claim_incarnation = NULL,
        claim_token = NULL, claim_started_at = NULL, claim_expires_at = NULL, stable_error_code = NULL, updated_at = reaped_at
      WHERE tenant_id = candidate.tenant_id AND event_id = candidate.event_id;
      UPDATE cloud_agents.platform_operations SET state = 'reconciling', updated_at = reaped_at
      WHERE tenant_id = candidate.tenant_id AND operation_id = candidate.operation_id
        AND operation_generation = candidate.operation_generation;
    ELSE
      outbox_state := 'dead_letter';
      SELECT revision.current_revision INTO expected_revision FROM cloud_agents.tenant_resource_versions AS revision
      WHERE revision.tenant_id = candidate.tenant_id AND revision.tenant_uid = candidate.tenant_id FOR UPDATE;
      next_revision := cloud_agents.allocate_tenant_revision(candidate.tenant_id, expected_revision, reaped_at);
      INSERT INTO cloud_agents.resource_changes
        (tenant_id, tenant_uid, resource_version, resource_kind, resource_uid, change_kind, actor_database_principal, occurred_at)
      VALUES (candidate.tenant_id, candidate.tenant_id, next_revision, 'workspaceSnapshot', candidate.aggregate_id,
        'updated', actor, reaped_at);
      UPDATE cloud_agents.workspace_snapshots SET status = 'cleanup_failed', stable_error_code = error_code,
        resource_version = next_revision, updated_at = reaped_at, observed_at = reaped_at
      WHERE tenant_id = candidate.tenant_id AND cleanup_operation_id = candidate.operation_id
        AND snapshot_uid = candidate.aggregate_id;
      UPDATE cloud_agents.outbox_events SET state = 'dead_letter', claim_holder_id = NULL, claim_incarnation = NULL,
        claim_token = NULL, claim_started_at = NULL, claim_expires_at = NULL, stable_error_code = error_code,
        dead_lettered_at = reaped_at, updated_at = reaped_at
      WHERE tenant_id = candidate.tenant_id AND event_id = candidate.event_id;
      UPDATE cloud_agents.platform_operations SET state = 'failed', cleanup_phase = 'blocked', terminal_error_code = error_code,
        terminal_resource_kind = 'workspaceSnapshot', terminal_resource_id = candidate.aggregate_id,
        terminal_resource_version = next_revision, updated_at = reaped_at, terminal_at = reaped_at
      WHERE tenant_id = candidate.tenant_id AND operation_id = candidate.operation_id
        AND operation_generation = candidate.operation_generation;
      UPDATE cloud_agents.operation_finalizers SET state = 'dead_letter', delivery_attempts = candidate.delivery_attempts,
        stable_error_code = error_code, updated_at = reaped_at, terminal_at = reaped_at
      WHERE tenant_id = candidate.tenant_id AND operation_id = candidate.operation_id
        AND operation_generation = candidate.operation_generation;
      UPDATE cloud_agents.idempotency_records SET state = 'failed', stable_error_code = error_code,
        resource_kind = 'workspaceSnapshot', resource_id = candidate.aggregate_id, resource_version = next_revision,
        updated_at = reaped_at, terminal_at = reaped_at
      WHERE tenant_id = candidate.tenant_id AND operation_id = candidate.operation_id
        AND operation_generation = candidate.operation_generation;
      INSERT INTO cloud_agents.terminal_receipts
        (tenant_id, tenant_ref_id, operation_id, operation_generation, attempt_number,
         receipt_id, outcome, stable_error_code, persisted_at)
      VALUES (candidate.tenant_id, candidate.tenant_id, candidate.operation_id, candidate.operation_generation,
        candidate.delivery_attempts, 'expired', 'failed', error_code, reaped_at);
    END IF;
    PERFORM cloud_agents.append_coordination_audit(candidate.tenant_id, p_audit, candidate.profile_id,
      candidate.profile_digest, p_subject, candidate.operation_id, candidate.operation_generation,
      candidate.delivery_attempts, NULL, NULL, NULL, 'workspace.snapshot.cleanup.reap',
      CASE WHEN candidate.delivery_attempts < 8 THEN 'pending' ELSE 'failed' END,
      CASE WHEN candidate.delivery_attempts < 8 THEN NULL ELSE error_code END, NULL, reaped_at);
    tenant_id := candidate.tenant_id; event_id := candidate.event_id;
    delivery_attempts := candidate.delivery_attempts; RETURN NEXT;
END;
$$;

ALTER FUNCTION cloud_agents.accept_foundation_workspace_snapshot_v2(text,text,text,bigint,integer,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.reject_expired_workspace_snapshot_restore_v1() OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.accept_foundation_workspace_snapshot_cleanup_v1(text,text,bigint,text,text,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.expire_foundation_workspace_snapshot_v1(text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.claim_foundation_workspace_snapshot_cleanup_v1(text,text,text,integer,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.renew_foundation_workspace_snapshot_cleanup_claim_v1(text,text,text,text,text,timestamptz,integer) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.settle_foundation_workspace_snapshot_cleanup_v1(text,text,text,text,text,timestamptz,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.reap_foundation_workspace_snapshot_cleanup_claim_v1(text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_workspace_snapshot_v2(text,text,text,bigint,integer,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.accept_foundation_workspace_snapshot_cleanup_v1(text,text,bigint,text,text,text,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.expire_foundation_workspace_snapshot_v1(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_foundation_workspace_snapshot_cleanup_v1(text,text,text,integer,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.renew_foundation_workspace_snapshot_cleanup_claim_v1(text,text,text,text,text,timestamptz,integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.settle_foundation_workspace_snapshot_cleanup_v1(text,text,text,text,text,timestamptz,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.reap_foundation_workspace_snapshot_cleanup_claim_v1(text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_workspace_snapshot_v2(text,text,text,bigint,integer,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.accept_foundation_workspace_snapshot_cleanup_v1(text,text,bigint,text,text,text,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.expire_foundation_workspace_snapshot_v1(text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_foundation_workspace_snapshot_cleanup_v1(text,text,text,integer,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.renew_foundation_workspace_snapshot_cleanup_claim_v1(text,text,text,text,text,timestamptz,integer) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.settle_foundation_workspace_snapshot_cleanup_v1(text,text,text,text,text,timestamptz,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.reap_foundation_workspace_snapshot_cleanup_claim_v1(text,text) TO cloud_agents_runtime;
