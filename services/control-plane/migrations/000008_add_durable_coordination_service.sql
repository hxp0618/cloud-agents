-- P1-A2.3 slice 3: generated-profile idempotency, outbox claim and leader
-- fencing entry points. This forward migration does not expose HTTP or any
-- Worker/P2 actuator. Runtime retains zero direct table DML.

CREATE FUNCTION cloud_agents.append_coordination_audit(
    p_tenant_id text,
    p_audit_fact_id text,
    p_profile_id text,
    p_profile_digest text,
    p_subject_digest text,
    p_operation_id text,
    p_operation_generation bigint,
    p_attempt_number bigint,
    p_resource_kind text,
    p_resource_id text,
    p_resource_version bigint,
    p_transition text,
    p_outcome text,
    p_stable_error_code text,
    p_fencing_token bigint,
    p_database_timestamp timestamptz
)
RETURNS void
LANGUAGE plpgsql
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
BEGIN
    INSERT INTO cloud_agents.coordination_audit_facts (
        tenant_id,
        tenant_ref_id,
        audit_fact_id,
        registry_digest,
        profile_id,
        profile_digest,
        subject_digest,
        operation_id,
        operation_generation,
        attempt_number,
        resource_kind,
        resource_id,
        resource_version,
        transition,
        outcome,
        stable_error_code,
        fencing_token,
        database_timestamp
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        p_audit_fact_id,
        cloud_agents.coordination_registry_digest(),
        p_profile_id,
        p_profile_digest,
        p_subject_digest,
        p_operation_id,
        p_operation_generation,
        p_attempt_number,
        p_resource_kind,
        p_resource_id,
        p_resource_version,
        p_transition,
        p_outcome,
        p_stable_error_code,
        p_fencing_token,
        p_database_timestamp
    );
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.claim_managed_agent_create_project_idempotency(
    p_tenant_id text,
    p_subject_digest text,
    p_idempotency_key text,
    p_request_digest text,
    p_audit_fact_id text
)
RETURNS TABLE (
    claim_disposition text,
    replay_state text,
    operation_id text,
    operation_generation bigint,
    resource_kind text,
    resource_id text,
    resource_version bigint,
    stable_error_code text,
    expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    expected_profile_id constant text := 'managedAgentCreateProject/v1alpha1';
    expected_profile_digest constant text := 'sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a';
    claimed_at timestamptz;
    inserted_count bigint;
    stored record;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    claimed_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.char_length(p_idempotency_key) NOT BETWEEN 16 AND 128
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]+$'
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_id)
        OR NOT cloud_agents.coordination_profile_is_registered(expected_profile_id, expected_profile_digest)
        OR cloud_agents.coordination_profile_creates_operation(expected_profile_id, expected_profile_digest)
        OR cloud_agents.coordination_profile_outbox_class(expected_profile_id, expected_profile_digest) <> 'resource_change'
        OR cloud_agents.coordination_profile_replay_ttl_seconds(expected_profile_id, expected_profile_digest) <> 86400
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'idempotency claim input is invalid';
    END IF;

    INSERT INTO cloud_agents.idempotency_records (
        tenant_id,
        tenant_ref_id,
        subject_digest,
        registry_digest,
        profile_id,
        profile_digest,
        idempotency_key,
        request_digest,
        state,
        created_at,
        updated_at,
        expires_at
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        p_subject_digest,
        cloud_agents.coordination_registry_digest(),
        expected_profile_id,
        expected_profile_digest,
        p_idempotency_key,
        p_request_digest,
        'pending',
        claimed_at,
        claimed_at,
        claimed_at + interval '86400 seconds'
    ) ON CONFLICT (
        tenant_id,
        subject_digest,
        profile_id,
        profile_digest,
        idempotency_key
    ) DO NOTHING;
    GET DIAGNOSTICS inserted_count = ROW_COUNT;

    SELECT record.*
    INTO STRICT stored
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = p_tenant_id
        AND record.subject_digest = p_subject_digest
        AND record.profile_id = expected_profile_id
        AND record.profile_digest = expected_profile_digest
        AND record.idempotency_key = p_idempotency_key
    FOR UPDATE;

    IF inserted_count = 1 THEN
        PERFORM cloud_agents.append_coordination_audit(
            p_tenant_id,
            p_audit_fact_id,
            expected_profile_id,
            expected_profile_digest,
            p_subject_digest,
            NULL,
            NULL,
            NULL,
            NULL,
            NULL,
            NULL,
            'idempotency.claim',
            'pending',
            NULL,
            NULL,
            claimed_at
        );
        claim_disposition := 'created';
    ELSIF stored.request_digest IS DISTINCT FROM p_request_digest THEN
        claim_disposition := 'conflict';
    ELSE
        claim_disposition := 'replay';
    END IF;

    replay_state := stored.state;
    operation_id := stored.operation_id;
    operation_generation := stored.operation_generation;
    resource_kind := stored.resource_kind;
    resource_id := stored.resource_id;
    resource_version := stored.resource_version;
    stable_error_code := stored.stable_error_code;
    expires_at := stored.expires_at;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.complete_managed_agent_create_project_success(
    p_tenant_id text,
    p_subject_digest text,
    p_idempotency_key text,
    p_request_digest text,
    p_resource_id text,
    p_resource_version bigint,
    p_event_id text,
    p_payload_digest text,
    p_audit_fact_id text
)
RETURNS TABLE (
    replay_state text,
    resource_kind text,
    resource_id text,
    resource_version bigint,
    outbox_event_id text,
    outbox_state text
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    expected_profile_id constant text := 'managedAgentCreateProject/v1alpha1';
    expected_profile_digest constant text := 'sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a';
    completed_at timestamptz;
    stored record;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    completed_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.char_length(p_idempotency_key) NOT BETWEEN 16 AND 128
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]+$'
        OR NOT cloud_agents.is_valid_identifier(p_resource_id)
        OR p_resource_version < 1
        OR NOT cloud_agents.is_valid_identifier(p_event_id)
        OR p_payload_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_id)
        OR NOT cloud_agents.coordination_profile_is_registered(expected_profile_id, expected_profile_digest)
        OR cloud_agents.coordination_profile_creates_operation(expected_profile_id, expected_profile_digest)
        OR cloud_agents.coordination_profile_outbox_class(expected_profile_id, expected_profile_digest) <> 'resource_change'
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'idempotency success input is invalid';
    END IF;

    SELECT record.*
    INTO stored
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = p_tenant_id
        AND record.subject_digest = p_subject_digest
        AND record.profile_id = expected_profile_id
        AND record.profile_digest = expected_profile_digest
        AND record.idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF NOT FOUND
        OR stored.request_digest IS DISTINCT FROM p_request_digest
        OR stored.state IS DISTINCT FROM 'pending'
    THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'idempotency success claim is not pending';
    END IF;

    PERFORM 1
    FROM cloud_agents.resource_changes AS change
    WHERE change.tenant_id = p_tenant_id
        AND change.resource_version = p_resource_version
        AND change.resource_kind = 'project'
        AND change.resource_uid = p_resource_id
    FOR KEY SHARE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'idempotency success resource is absent';
    END IF;

    INSERT INTO cloud_agents.outbox_events (
        tenant_id,
        tenant_ref_id,
        event_id,
        registry_digest,
        profile_id,
        profile_digest,
        event_class,
        aggregate_kind,
        aggregate_id,
        aggregate_sequence,
        resource_version,
        generation,
        operation_id,
        operation_generation,
        payload_digest,
        state,
        delivery_attempts,
        created_at,
        updated_at
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        p_event_id,
        cloud_agents.coordination_registry_digest(),
        expected_profile_id,
        expected_profile_digest,
        'resource_change',
        'project',
        p_resource_id,
        p_resource_version,
        p_resource_version,
        0,
        NULL,
        NULL,
        p_payload_digest,
        'pending',
        0,
        completed_at,
        completed_at
    );

    UPDATE cloud_agents.idempotency_records AS record
    SET
        state = 'succeeded',
        resource_kind = 'project',
        resource_id = p_resource_id,
        resource_version = p_resource_version,
        updated_at = completed_at,
        terminal_at = completed_at
    WHERE record.tenant_id = p_tenant_id
        AND record.subject_digest = p_subject_digest
        AND record.profile_id = expected_profile_id
        AND record.profile_digest = expected_profile_digest
        AND record.idempotency_key = p_idempotency_key;

    PERFORM cloud_agents.append_coordination_audit(
        p_tenant_id,
        p_audit_fact_id,
        expected_profile_id,
        expected_profile_digest,
        p_subject_digest,
        NULL,
        NULL,
        NULL,
        'project',
        p_resource_id,
        p_resource_version,
        'idempotency.record_success',
        'succeeded',
        NULL,
        NULL,
        completed_at
    );

    replay_state := 'succeeded';
    resource_kind := 'project';
    resource_id := p_resource_id;
    resource_version := p_resource_version;
    outbox_event_id := p_event_id;
    outbox_state := 'pending';
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.complete_managed_agent_create_project_failure(
    p_tenant_id text,
    p_subject_digest text,
    p_idempotency_key text,
    p_request_digest text,
    p_stable_error_code text,
    p_audit_fact_id text
)
RETURNS TABLE (replay_state text, stable_error_code text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    expected_profile_id constant text := 'managedAgentCreateProject/v1alpha1';
    expected_profile_digest constant text := 'sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a';
    completed_at timestamptz;
    stored record;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    completed_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_request_digest !~ '^sha256:[0-9a-f]{64}$'
        OR pg_catalog.char_length(p_idempotency_key) NOT BETWEEN 16 AND 128
        OR p_idempotency_key !~ '^[A-Za-z0-9._~-]+$'
        OR NOT cloud_agents.is_valid_identifier(p_stable_error_code)
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_id)
        OR NOT cloud_agents.coordination_profile_is_registered(expected_profile_id, expected_profile_digest)
        OR cloud_agents.coordination_profile_creates_operation(expected_profile_id, expected_profile_digest)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'idempotency failure input is invalid';
    END IF;

    SELECT record.*
    INTO stored
    FROM cloud_agents.idempotency_records AS record
    WHERE record.tenant_id = p_tenant_id
        AND record.subject_digest = p_subject_digest
        AND record.profile_id = expected_profile_id
        AND record.profile_digest = expected_profile_digest
        AND record.idempotency_key = p_idempotency_key
    FOR UPDATE;
    IF NOT FOUND
        OR stored.request_digest IS DISTINCT FROM p_request_digest
        OR stored.state IS DISTINCT FROM 'pending'
    THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'idempotency failure claim is not pending';
    END IF;

    UPDATE cloud_agents.idempotency_records AS record
    SET
        state = 'failed',
        stable_error_code = p_stable_error_code,
        updated_at = completed_at,
        terminal_at = completed_at
    WHERE record.tenant_id = p_tenant_id
        AND record.subject_digest = p_subject_digest
        AND record.profile_id = expected_profile_id
        AND record.profile_digest = expected_profile_digest
        AND record.idempotency_key = p_idempotency_key;

    PERFORM cloud_agents.append_coordination_audit(
        p_tenant_id,
        p_audit_fact_id,
        expected_profile_id,
        expected_profile_digest,
        p_subject_digest,
        NULL,
        NULL,
        NULL,
        NULL,
        NULL,
        NULL,
        'idempotency.record_failure',
        'failed',
        p_stable_error_code,
        NULL,
        completed_at
    );
    replay_state := 'failed';
    stable_error_code := p_stable_error_code;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.acquire_coordination_leader(
    p_leader_name text,
    p_holder_id text,
    p_holder_incarnation text,
    p_lease_seconds integer
)
RETURNS TABLE (
    lease_disposition text,
    fencing_token bigint,
    lease_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    acquired_at timestamptz;
    stored record;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    acquired_at := pg_catalog.transaction_timestamp();
    IF p_leader_name NOT IN ('coordination-reconciler', 'finalizer-reconciler', 'outbox-dispatcher')
        OR NOT cloud_agents.is_valid_identifier(p_holder_id)
        OR NOT cloud_agents.is_valid_identifier(p_holder_incarnation)
        OR p_lease_seconds NOT BETWEEN 1 AND 60
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'leader acquisition input is invalid';
    END IF;

    SELECT lease.*
    INTO stored
    FROM cloud_agents.leader_leases AS lease
    WHERE lease.leader_name = p_leader_name
    FOR UPDATE;

    IF NOT FOUND THEN
        INSERT INTO cloud_agents.leader_leases (
            leader_name,
            holder_id,
            holder_incarnation,
            fencing_token,
            lease_started_at,
            lease_expires_at,
            updated_at
        ) VALUES (
            p_leader_name,
            p_holder_id,
            p_holder_incarnation,
            1,
            acquired_at,
            acquired_at + pg_catalog.make_interval(secs => p_lease_seconds),
            acquired_at
        );
        lease_disposition := 'acquired';
        fencing_token := 1;
        lease_expires_at := acquired_at + pg_catalog.make_interval(secs => p_lease_seconds);
        RETURN NEXT;
        RETURN;
    END IF;

    IF stored.lease_expires_at > acquired_at THEN
        lease_disposition := 'busy';
        fencing_token := stored.fencing_token;
        lease_expires_at := stored.lease_expires_at;
        RETURN NEXT;
        RETURN;
    END IF;
    IF stored.fencing_token = 9223372036854775807 THEN
        RAISE EXCEPTION USING ERRCODE = '22003', MESSAGE = 'leader fencing token exhausted';
    END IF;

    UPDATE cloud_agents.leader_leases AS lease
    SET
        holder_id = p_holder_id,
        holder_incarnation = p_holder_incarnation,
        fencing_token = lease.fencing_token + 1,
        lease_started_at = acquired_at,
        lease_expires_at = acquired_at + pg_catalog.make_interval(secs => p_lease_seconds),
        updated_at = acquired_at
    WHERE lease.leader_name = p_leader_name
    RETURNING lease.fencing_token, lease.lease_expires_at
    INTO fencing_token, lease_expires_at;
    lease_disposition := 'acquired';
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.renew_coordination_leader(
    p_leader_name text,
    p_holder_id text,
    p_holder_incarnation text,
    p_fencing_token bigint,
    p_lease_seconds integer
)
RETURNS TABLE (
    lease_disposition text,
    fencing_token bigint,
    lease_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    renewed_at timestamptz;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    renewed_at := pg_catalog.transaction_timestamp();
    IF p_leader_name NOT IN ('coordination-reconciler', 'finalizer-reconciler', 'outbox-dispatcher')
        OR NOT cloud_agents.is_valid_identifier(p_holder_id)
        OR NOT cloud_agents.is_valid_identifier(p_holder_incarnation)
        OR p_fencing_token < 1
        OR p_lease_seconds NOT BETWEEN 1 AND 60
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'leader renewal input is invalid';
    END IF;

    UPDATE cloud_agents.leader_leases AS lease
    SET
        lease_started_at = renewed_at,
        lease_expires_at = renewed_at + pg_catalog.make_interval(secs => p_lease_seconds),
        updated_at = renewed_at
    WHERE lease.leader_name = p_leader_name
        AND lease.holder_id = p_holder_id
        AND lease.holder_incarnation = p_holder_incarnation
        AND lease.fencing_token = p_fencing_token
        AND lease.lease_expires_at > renewed_at
    RETURNING lease.fencing_token, lease.lease_expires_at
    INTO fencing_token, lease_expires_at;
    IF FOUND THEN
        lease_disposition := 'renewed';
    ELSE
        lease_disposition := 'rejected';
        fencing_token := NULL;
        lease_expires_at := NULL;
    END IF;
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.claim_outbox_event(
    p_tenant_id text,
    p_holder_id text,
    p_holder_incarnation text,
    p_claim_token text,
    p_lease_seconds integer,
    p_subject_digest text,
    p_audit_fact_id text
)
RETURNS TABLE (
    event_id text,
    profile_id text,
    profile_digest text,
    event_class text,
    aggregate_kind text,
    aggregate_id text,
    aggregate_sequence bigint,
    resource_version bigint,
    generation bigint,
    operation_id text,
    operation_generation bigint,
    payload_digest text,
    delivery_attempts integer,
    claim_expires_at timestamptz
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    claimed_at timestamptz;
    candidate record;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    claimed_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_holder_id)
        OR NOT cloud_agents.is_valid_identifier(p_holder_incarnation)
        OR NOT cloud_agents.is_valid_identifier(p_claim_token)
        OR p_lease_seconds NOT BETWEEN 1 AND 60
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'outbox claim input is invalid';
    END IF;

    SELECT event.*
    INTO candidate
    FROM cloud_agents.outbox_events AS event
    WHERE event.tenant_id = p_tenant_id
        AND (
            event.state = 'pending'
            OR (event.state = 'retry_wait' AND event.next_attempt_at <= claimed_at)
        )
        AND event.delivery_attempts < 8
    ORDER BY event.created_at, event.event_id
    FOR UPDATE SKIP LOCKED
    LIMIT 1;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    UPDATE cloud_agents.outbox_events AS event
    SET
        state = 'claimed',
        delivery_attempts = event.delivery_attempts + 1,
        next_attempt_at = NULL,
        claim_holder_id = p_holder_id,
        claim_incarnation = p_holder_incarnation,
        claim_token = p_claim_token,
        claim_started_at = claimed_at,
        claim_expires_at = claimed_at + pg_catalog.make_interval(secs => p_lease_seconds),
        updated_at = claimed_at
    WHERE event.tenant_id = p_tenant_id
        AND event.event_id = candidate.event_id
    RETURNING
        event.event_id,
        event.profile_id,
        event.profile_digest,
        event.event_class,
        event.aggregate_kind,
        event.aggregate_id,
        event.aggregate_sequence,
        event.resource_version,
        event.generation,
        event.operation_id,
        event.operation_generation,
        event.payload_digest,
        event.delivery_attempts,
        event.claim_expires_at
    INTO
        event_id,
        profile_id,
        profile_digest,
        event_class,
        aggregate_kind,
        aggregate_id,
        aggregate_sequence,
        resource_version,
        generation,
        operation_id,
        operation_generation,
        payload_digest,
        delivery_attempts,
        claim_expires_at;

    PERFORM cloud_agents.append_coordination_audit(
        p_tenant_id,
        p_audit_fact_id,
        profile_id,
        profile_digest,
        p_subject_digest,
        operation_id,
        operation_generation,
        NULL,
        CASE WHEN event_class = 'resource_change' THEN aggregate_kind END,
        CASE WHEN event_class = 'resource_change' THEN aggregate_id END,
        resource_version,
        'outbox.claim',
        'pending',
        NULL,
        NULL,
        claimed_at
    );
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.transition_outbox_claim(
    p_tenant_id text,
    p_event_id text,
    p_holder_id text,
    p_holder_incarnation text,
    p_claim_token text,
    p_claim_expires_at timestamptz,
    p_transition text,
    p_stable_error_code text,
    p_subject_digest text,
    p_audit_fact_id text
)
RETURNS TABLE (event_id text, outbox_state text, delivery_attempts integer, next_attempt_at timestamptz)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    settled_at timestamptz;
    stored record;
    retry_seconds integer;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    settled_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_event_id)
        OR NOT cloud_agents.is_valid_identifier(p_holder_id)
        OR NOT cloud_agents.is_valid_identifier(p_holder_incarnation)
        OR NOT cloud_agents.is_valid_identifier(p_claim_token)
        OR p_claim_expires_at IS NULL
        OR p_transition NOT IN ('delivery_succeeded', 'delivery_failed_retryable', 'delivery_failed_terminal')
        OR (p_transition = 'delivery_failed_terminal' AND NOT cloud_agents.is_valid_identifier(p_stable_error_code))
        OR (p_transition <> 'delivery_failed_terminal' AND p_stable_error_code IS NOT NULL)
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'outbox settlement input is invalid';
    END IF;

    SELECT event.*
    INTO stored
    FROM cloud_agents.outbox_events AS event
    WHERE event.tenant_id = p_tenant_id
        AND event.event_id = p_event_id
    FOR UPDATE;
    IF NOT FOUND
        OR stored.state IS DISTINCT FROM 'claimed'
        OR stored.claim_holder_id IS DISTINCT FROM p_holder_id
        OR stored.claim_incarnation IS DISTINCT FROM p_holder_incarnation
        OR stored.claim_token IS DISTINCT FROM p_claim_token
        OR stored.claim_expires_at IS DISTINCT FROM p_claim_expires_at
        OR stored.claim_expires_at <= settled_at
        OR (p_transition = 'delivery_failed_retryable' AND stored.delivery_attempts >= 8)
    THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'outbox claim tuple is stale';
    END IF;

    IF p_transition = 'delivery_succeeded' THEN
        outbox_state := 'delivered';
        UPDATE cloud_agents.outbox_events AS event
        SET
            state = outbox_state,
            claim_holder_id = NULL,
            claim_incarnation = NULL,
            claim_token = NULL,
            claim_started_at = NULL,
            claim_expires_at = NULL,
            delivered_at = settled_at,
            updated_at = settled_at
        WHERE event.tenant_id = p_tenant_id AND event.event_id = p_event_id;
    ELSIF p_transition = 'delivery_failed_retryable' THEN
        outbox_state := 'retry_wait';
        retry_seconds := CASE stored.delivery_attempts
            WHEN 1 THEN 1 WHEN 2 THEN 2 WHEN 3 THEN 4 WHEN 4 THEN 8
            WHEN 5 THEN 16 WHEN 6 THEN 32 WHEN 7 THEN 64 ELSE 128
        END;
        next_attempt_at := settled_at + pg_catalog.make_interval(secs => retry_seconds);
        UPDATE cloud_agents.outbox_events AS event
        SET
            state = outbox_state,
            next_attempt_at = transition_outbox_claim.next_attempt_at,
            claim_holder_id = NULL,
            claim_incarnation = NULL,
            claim_token = NULL,
            claim_started_at = NULL,
            claim_expires_at = NULL,
            updated_at = settled_at
        WHERE event.tenant_id = p_tenant_id AND event.event_id = p_event_id;
    ELSE
        outbox_state := 'dead_letter';
        UPDATE cloud_agents.outbox_events AS event
        SET
            state = outbox_state,
            claim_holder_id = NULL,
            claim_incarnation = NULL,
            claim_token = NULL,
            claim_started_at = NULL,
            claim_expires_at = NULL,
            stable_error_code = p_stable_error_code,
            dead_lettered_at = settled_at,
            updated_at = settled_at
        WHERE event.tenant_id = p_tenant_id AND event.event_id = p_event_id;
    END IF;

    event_id := p_event_id;
    delivery_attempts := stored.delivery_attempts;
    PERFORM cloud_agents.append_coordination_audit(
        p_tenant_id,
        p_audit_fact_id,
        stored.profile_id,
        stored.profile_digest,
        p_subject_digest,
        stored.operation_id,
        stored.operation_generation,
        NULL,
        CASE WHEN stored.event_class = 'resource_change' THEN stored.aggregate_kind END,
        CASE WHEN stored.event_class = 'resource_change' THEN stored.aggregate_id END,
        stored.resource_version,
        'outbox.' || p_transition,
        CASE WHEN outbox_state = 'delivered' THEN 'succeeded' ELSE 'failed' END,
        p_stable_error_code,
        NULL,
        settled_at
    );
    RETURN NEXT;
END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.acknowledge_outbox_event(
    p_tenant_id text,
    p_event_id text,
    p_holder_id text,
    p_holder_incarnation text,
    p_claim_token text,
    p_claim_expires_at timestamptz,
    p_subject_digest text,
    p_audit_fact_id text
)
RETURNS TABLE (event_id text, outbox_state text, delivery_attempts integer, next_attempt_at timestamptz)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.transition_outbox_claim(
        p_tenant_id, p_event_id, p_holder_id, p_holder_incarnation,
        p_claim_token, p_claim_expires_at, 'delivery_succeeded', NULL,
        p_subject_digest, p_audit_fact_id
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.retry_outbox_event(
    p_tenant_id text,
    p_event_id text,
    p_holder_id text,
    p_holder_incarnation text,
    p_claim_token text,
    p_claim_expires_at timestamptz,
    p_subject_digest text,
    p_audit_fact_id text
)
RETURNS TABLE (event_id text, outbox_state text, delivery_attempts integer, next_attempt_at timestamptz)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.transition_outbox_claim(
        p_tenant_id, p_event_id, p_holder_id, p_holder_incarnation,
        p_claim_token, p_claim_expires_at, 'delivery_failed_retryable', NULL,
        p_subject_digest, p_audit_fact_id
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.dead_letter_outbox_event(
    p_tenant_id text,
    p_event_id text,
    p_holder_id text,
    p_holder_incarnation text,
    p_claim_token text,
    p_claim_expires_at timestamptz,
    p_stable_error_code text,
    p_subject_digest text,
    p_audit_fact_id text
)
RETURNS TABLE (event_id text, outbox_state text, delivery_attempts integer, next_attempt_at timestamptz)
LANGUAGE sql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT *
    FROM cloud_agents.transition_outbox_claim(
        p_tenant_id, p_event_id, p_holder_id, p_holder_incarnation,
        p_claim_token, p_claim_expires_at, 'delivery_failed_terminal',
        p_stable_error_code, p_subject_digest, p_audit_fact_id
    )
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.reap_expired_outbox_claim(
    p_tenant_id text,
    p_leader_holder_id text,
    p_leader_holder_incarnation text,
    p_fencing_token bigint,
    p_subject_digest text,
    p_audit_fact_id text
)
RETURNS TABLE (event_id text, outbox_state text, delivery_attempts integer)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    reaped_at timestamptz;
    candidate record;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    reaped_at := pg_catalog.transaction_timestamp();
    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR NOT cloud_agents.is_valid_identifier(p_leader_holder_id)
        OR NOT cloud_agents.is_valid_identifier(p_leader_holder_incarnation)
        OR p_fencing_token < 1
        OR p_subject_digest !~ '^sha256:[0-9a-f]{64}$'
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_id)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'outbox reaper input is invalid';
    END IF;

    PERFORM 1
    FROM cloud_agents.leader_leases AS lease
    WHERE lease.leader_name = 'outbox-dispatcher'
        AND lease.holder_id = p_leader_holder_id
        AND lease.holder_incarnation = p_leader_holder_incarnation
        AND lease.fencing_token = p_fencing_token
        AND lease.lease_expires_at > reaped_at
    FOR UPDATE;
    IF NOT FOUND THEN
        RAISE EXCEPTION USING ERRCODE = '40001', MESSAGE = 'outbox reaper leader fence is stale';
    END IF;

    SELECT event.*
    INTO candidate
    FROM cloud_agents.outbox_events AS event
    WHERE event.tenant_id = p_tenant_id
        AND event.state = 'claimed'
        AND event.claim_expires_at <= reaped_at
    ORDER BY event.claim_expires_at, event.event_id
    FOR UPDATE SKIP LOCKED
    LIMIT 1;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    event_id := candidate.event_id;
    delivery_attempts := candidate.delivery_attempts;
    IF candidate.delivery_attempts >= 8 THEN
        outbox_state := 'dead_letter';
        UPDATE cloud_agents.outbox_events AS event
        SET
            state = outbox_state,
            claim_holder_id = NULL,
            claim_incarnation = NULL,
            claim_token = NULL,
            claim_started_at = NULL,
            claim_expires_at = NULL,
            stable_error_code = 'delivery_attempts_exhausted',
            dead_lettered_at = reaped_at,
            updated_at = reaped_at
        WHERE event.tenant_id = p_tenant_id AND event.event_id = candidate.event_id;
    ELSE
        outbox_state := 'pending';
        UPDATE cloud_agents.outbox_events AS event
        SET
            state = outbox_state,
            claim_holder_id = NULL,
            claim_incarnation = NULL,
            claim_token = NULL,
            claim_started_at = NULL,
            claim_expires_at = NULL,
            updated_at = reaped_at
        WHERE event.tenant_id = p_tenant_id AND event.event_id = candidate.event_id;
    END IF;

    PERFORM cloud_agents.append_coordination_audit(
        p_tenant_id,
        p_audit_fact_id,
        candidate.profile_id,
        candidate.profile_digest,
        p_subject_digest,
        candidate.operation_id,
        candidate.operation_generation,
        NULL,
        CASE WHEN candidate.event_class = 'resource_change' THEN candidate.aggregate_kind END,
        CASE WHEN candidate.event_class = 'resource_change' THEN candidate.aggregate_id END,
        candidate.resource_version,
        CASE WHEN outbox_state = 'pending' THEN 'outbox.lease_expired_retryable' ELSE 'outbox.lease_expired_terminal' END,
        CASE WHEN outbox_state = 'pending' THEN 'pending' ELSE 'failed' END,
        CASE WHEN outbox_state = 'dead_letter' THEN 'delivery_attempts_exhausted' END,
        p_fencing_token,
        reaped_at
    );
    RETURN NEXT;
END
$cloud_agents_function$;

REVOKE ALL ON FUNCTION cloud_agents.append_coordination_audit(
    text, text, text, text, text, text, bigint, bigint, text, text, bigint,
    text, text, text, bigint, timestamptz
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_managed_agent_create_project_idempotency(
    text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_managed_agent_create_project_success(
    text, text, text, text, text, bigint, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.complete_managed_agent_create_project_failure(
    text, text, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.acquire_coordination_leader(text, text, text, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.renew_coordination_leader(text, text, text, bigint, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.claim_outbox_event(
    text, text, text, text, integer, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.transition_outbox_claim(
    text, text, text, text, text, timestamptz, text, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.acknowledge_outbox_event(
    text, text, text, text, text, timestamptz, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.retry_outbox_event(
    text, text, text, text, text, timestamptz, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.dead_letter_outbox_event(
    text, text, text, text, text, timestamptz, text, text, text
) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.reap_expired_outbox_claim(
    text, text, text, bigint, text, text
) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION cloud_agents.claim_managed_agent_create_project_idempotency(
    text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_managed_agent_create_project_success(
    text, text, text, text, text, bigint, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.complete_managed_agent_create_project_failure(
    text, text, text, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.acquire_coordination_leader(text, text, text, integer)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.renew_coordination_leader(text, text, text, bigint, integer)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.claim_outbox_event(
    text, text, text, text, integer, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.acknowledge_outbox_event(
    text, text, text, text, text, timestamptz, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.retry_outbox_event(
    text, text, text, text, text, timestamptz, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.dead_letter_outbox_event(
    text, text, text, text, text, timestamptz, text, text, text
) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.reap_expired_outbox_claim(
    text, text, text, bigint, text, text
) TO cloud_agents_runtime;
