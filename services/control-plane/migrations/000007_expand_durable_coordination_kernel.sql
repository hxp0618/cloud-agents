-- P1-A2.3 slice 2: append-only durable coordination schema kernel.
--
-- Exact generated authority:
--   registry digest: sha256:11c0f599e8320668a6f601241206c795933b26e3b9c456a58353a0d13c7ecd30
--   state-machine digest: sha256:5c4fa5c0cfac253b45a41c2e49ee7e863b9efbe124e5d743e041f5e01f5c6f15
--   policy digest: sha256:95023973eb007a958a3c5aea3ac61b6caa7cd8955b9a24fcef3ad269230c64e8
--   profile: managedAgentCreateProject/v1alpha1
--   profile digest: sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a
--
-- This migration creates no HTTP surface, no external actuator, and no
-- runtime write entry point. cloud_agents_runtime receives SELECT plus pure
-- generated-profile helper EXECUTE only. Slice 3 owns typed mutations.

CREATE FUNCTION cloud_agents.coordination_registry_digest()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:11c0f599e8320668a6f601241206c795933b26e3b9c456a58353a0d13c7ecd30'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.coordination_state_machine_digest()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:5c4fa5c0cfac253b45a41c2e49ee7e863b9efbe124e5d743e041f5e01f5c6f15'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.coordination_policy_digest()
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT 'sha256:95023973eb007a958a3c5aea3ac61b6caa7cd8955b9a24fcef3ad269230c64e8'::text
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.coordination_profile_is_registered(
    p_profile_id text,
    p_profile_digest text
)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT p_profile_id = 'managedAgentCreateProject/v1alpha1'
        AND p_profile_digest = 'sha256:059b4cca58f9621e9b70b723fb3b681f62948d6d4965af60105165afce680d5a'
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.coordination_profile_creates_operation(
    p_profile_id text,
    p_profile_digest text
)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT cloud_agents.coordination_profile_is_registered(p_profile_id, p_profile_digest)
        AND false
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.coordination_profile_outbox_class(
    p_profile_id text,
    p_profile_digest text
)
RETURNS text
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT CASE
        WHEN cloud_agents.coordination_profile_is_registered(p_profile_id, p_profile_digest)
            THEN 'resource_change'::text
        ELSE NULL::text
    END
$cloud_agents_function$;

CREATE FUNCTION cloud_agents.coordination_profile_replay_ttl_seconds(
    p_profile_id text,
    p_profile_digest text
)
RETURNS bigint
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
    SELECT CASE
        WHEN cloud_agents.coordination_profile_is_registered(p_profile_id, p_profile_digest)
            THEN 86400::bigint
        ELSE NULL::bigint
    END
$cloud_agents_function$;

CREATE TABLE cloud_agents.platform_operations (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    operation_id text NOT NULL,
    operation_generation bigint NOT NULL,
    registry_digest text NOT NULL,
    state_machine_digest text NOT NULL,
    policy_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    subject_digest text NOT NULL,
    request_digest text NOT NULL,
    state text NOT NULL,
    cleanup_phase text NOT NULL,
    recovery_generation bigint NOT NULL,
    current_attempt_number bigint NOT NULL,
    terminal_resource_kind text,
    terminal_resource_id text,
    terminal_resource_version bigint,
    terminal_error_code text,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    terminal_at timestamptz,
    PRIMARY KEY (tenant_id, operation_id, operation_generation),
    CONSTRAINT platform_operations_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT platform_operations_operation_id
        CHECK (cloud_agents.is_valid_identifier(operation_id)),
    CONSTRAINT platform_operations_generation CHECK (operation_generation > 0),
    CONSTRAINT platform_operations_registry_digest
        CHECK (registry_digest = cloud_agents.coordination_registry_digest()),
    CONSTRAINT platform_operations_state_machine_digest
        CHECK (state_machine_digest = cloud_agents.coordination_state_machine_digest()),
    CONSTRAINT platform_operations_policy_digest
        CHECK (policy_digest = cloud_agents.coordination_policy_digest()),
    CONSTRAINT platform_operations_profile
        CHECK (
            cloud_agents.coordination_profile_is_registered(profile_id, profile_digest)
            AND cloud_agents.coordination_profile_creates_operation(profile_id, profile_digest)
        ),
    CONSTRAINT platform_operations_subject_digest
        CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT platform_operations_request_digest
        CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT platform_operations_state
        CHECK (state IN ('pending', 'running', 'reconciling', 'succeeded', 'failed', 'canceled')),
    CONSTRAINT platform_operations_cleanup_phase
        CHECK (cleanup_phase IN ('none', 'pending', 'revoking', 'draining', 'reaping', 'complete', 'blocked')),
    CONSTRAINT platform_operations_recovery_generation CHECK (recovery_generation >= 0),
    CONSTRAINT platform_operations_attempt_number CHECK (current_attempt_number >= 0),
    CONSTRAINT platform_operations_terminal_envelope
        CHECK (
            (
                state IN ('pending', 'running', 'reconciling')
                AND terminal_resource_kind IS NULL
                AND terminal_resource_id IS NULL
                AND terminal_resource_version IS NULL
                AND terminal_error_code IS NULL
                AND terminal_at IS NULL
            )
            OR (
                state = 'succeeded'
                AND cloud_agents.is_valid_identifier(terminal_resource_kind)
                AND cloud_agents.is_valid_identifier(terminal_resource_id)
                AND terminal_resource_version > 0
                AND terminal_error_code IS NULL
                AND terminal_at IS NOT NULL
            )
            OR (
                state IN ('failed', 'canceled')
                AND terminal_resource_kind IS NULL
                AND terminal_resource_id IS NULL
                AND terminal_resource_version IS NULL
                AND cloud_agents.is_valid_identifier(terminal_error_code)
                AND terminal_at IS NOT NULL
            )
        ),
    CONSTRAINT platform_operations_cleanup_complete
        CHECK (cleanup_phase <> 'complete' OR state IN ('succeeded', 'failed', 'canceled')),
    CONSTRAINT platform_operations_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT platform_operations_terminal_after_created
        CHECK (terminal_at IS NULL OR terminal_at >= created_at),
    CONSTRAINT platform_operations_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT platform_operations_terminal_resource_fk
        FOREIGN KEY (
            tenant_id,
            terminal_resource_version,
            terminal_resource_kind,
            terminal_resource_id
        )
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX platform_operations_profile_state_idx
    ON cloud_agents.platform_operations
    (tenant_id, profile_id, profile_digest, state, updated_at, operation_id, operation_generation);

CREATE INDEX platform_operations_tenant_fk_idx
    ON cloud_agents.platform_operations (tenant_id, tenant_ref_id);

CREATE INDEX platform_operations_terminal_resource_fk_idx
    ON cloud_agents.platform_operations
    (tenant_id, terminal_resource_version, terminal_resource_kind, terminal_resource_id);

CREATE TABLE cloud_agents.operation_attempts (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    operation_id text NOT NULL,
    operation_generation bigint NOT NULL,
    attempt_number bigint NOT NULL,
    state text NOT NULL,
    claim_holder_id text,
    claim_incarnation text,
    claim_token text,
    claim_started_at timestamptz,
    claim_expires_at timestamptz,
    stable_error_code text,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    terminal_at timestamptz,
    PRIMARY KEY (tenant_id, operation_id, operation_generation, attempt_number),
    CONSTRAINT operation_attempts_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT operation_attempts_operation_id
        CHECK (cloud_agents.is_valid_identifier(operation_id)),
    CONSTRAINT operation_attempts_generation CHECK (operation_generation > 0),
    CONSTRAINT operation_attempts_number CHECK (attempt_number > 0),
    CONSTRAINT operation_attempts_state
        CHECK (state IN ('ready', 'claimed', 'unknown', 'succeeded', 'failed')),
    CONSTRAINT operation_attempts_claim_tuple
        CHECK (
            (
                state = 'claimed'
                AND cloud_agents.is_valid_identifier(claim_holder_id)
                AND cloud_agents.is_valid_identifier(claim_incarnation)
                AND cloud_agents.is_valid_identifier(claim_token)
                AND claim_started_at IS NOT NULL
                AND claim_expires_at > claim_started_at
            )
            OR (
                state <> 'claimed'
                AND claim_holder_id IS NULL
                AND claim_incarnation IS NULL
                AND claim_token IS NULL
                AND claim_started_at IS NULL
                AND claim_expires_at IS NULL
            )
        ),
    CONSTRAINT operation_attempts_terminal
        CHECK (
            (
                state IN ('ready', 'claimed', 'unknown')
                AND stable_error_code IS NULL
                AND terminal_at IS NULL
            )
            OR (
                state = 'succeeded'
                AND stable_error_code IS NULL
                AND terminal_at IS NOT NULL
            )
            OR (
                state = 'failed'
                AND cloud_agents.is_valid_identifier(stable_error_code)
                AND terminal_at IS NOT NULL
            )
        ),
    CONSTRAINT operation_attempts_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT operation_attempts_terminal_after_created
        CHECK (terminal_at IS NULL OR terminal_at >= created_at),
    CONSTRAINT operation_attempts_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT operation_attempts_operation_fk
        FOREIGN KEY (tenant_id, operation_id, operation_generation)
        REFERENCES cloud_agents.platform_operations
            (tenant_id, operation_id, operation_generation)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX operation_attempts_claim_idx
    ON cloud_agents.operation_attempts
    (tenant_id, state, claim_expires_at, operation_id, operation_generation, attempt_number);

CREATE INDEX operation_attempts_operation_fk_idx
    ON cloud_agents.operation_attempts
    (tenant_id, operation_id, operation_generation);

CREATE TABLE cloud_agents.terminal_receipts (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    operation_id text NOT NULL,
    operation_generation bigint NOT NULL,
    attempt_number bigint NOT NULL,
    receipt_id text NOT NULL,
    outcome text NOT NULL,
    resource_kind text,
    resource_id text,
    resource_version bigint,
    stable_error_code text,
    persisted_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (
        tenant_id,
        operation_id,
        operation_generation,
        attempt_number,
        receipt_id
    ),
    CONSTRAINT terminal_receipts_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT terminal_receipts_operation_id
        CHECK (cloud_agents.is_valid_identifier(operation_id)),
    CONSTRAINT terminal_receipts_generation CHECK (operation_generation > 0),
    CONSTRAINT terminal_receipts_attempt_number CHECK (attempt_number > 0),
    CONSTRAINT terminal_receipts_receipt_id
        CHECK (cloud_agents.is_valid_identifier(receipt_id)),
    CONSTRAINT terminal_receipts_outcome
        CHECK (outcome IN ('succeeded', 'failed', 'canceled')),
    CONSTRAINT terminal_receipts_envelope
        CHECK (
            (
                outcome = 'succeeded'
                AND cloud_agents.is_valid_identifier(resource_kind)
                AND cloud_agents.is_valid_identifier(resource_id)
                AND resource_version > 0
                AND stable_error_code IS NULL
            )
            OR (
                outcome IN ('failed', 'canceled')
                AND resource_kind IS NULL
                AND resource_id IS NULL
                AND resource_version IS NULL
                AND cloud_agents.is_valid_identifier(stable_error_code)
            )
        ),
    CONSTRAINT terminal_receipts_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT terminal_receipts_attempt_fk
        FOREIGN KEY (tenant_id, operation_id, operation_generation, attempt_number)
        REFERENCES cloud_agents.operation_attempts
            (tenant_id, operation_id, operation_generation, attempt_number)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT terminal_receipts_resource_fk
        FOREIGN KEY (tenant_id, resource_version, resource_kind, resource_id)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED,
    UNIQUE (tenant_id, operation_id, operation_generation, attempt_number)
);

CREATE INDEX terminal_receipts_attempt_fk_idx
    ON cloud_agents.terminal_receipts
    (tenant_id, operation_id, operation_generation, attempt_number);

CREATE INDEX terminal_receipts_resource_fk_idx
    ON cloud_agents.terminal_receipts
    (tenant_id, resource_version, resource_kind, resource_id);

CREATE TABLE cloud_agents.operation_finalizers (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    operation_id text NOT NULL,
    operation_generation bigint NOT NULL,
    finalizer_name text NOT NULL,
    required boolean NOT NULL,
    state text NOT NULL,
    delivery_attempts integer NOT NULL,
    next_attempt_at timestamptz,
    claim_holder_id text,
    claim_incarnation text,
    claim_token text,
    claim_started_at timestamptz,
    claim_expires_at timestamptz,
    stable_error_code text,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    terminal_at timestamptz,
    PRIMARY KEY (tenant_id, operation_id, operation_generation, finalizer_name),
    CONSTRAINT operation_finalizers_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT operation_finalizers_operation_id
        CHECK (cloud_agents.is_valid_identifier(operation_id)),
    CONSTRAINT operation_finalizers_generation CHECK (operation_generation > 0),
    CONSTRAINT operation_finalizers_name
        CHECK (cloud_agents.is_valid_identifier(finalizer_name)),
    CONSTRAINT operation_finalizers_state
        CHECK (state IN ('pending', 'claimed', 'retry_wait', 'unknown', 'succeeded', 'dead_letter')),
    CONSTRAINT operation_finalizers_delivery_attempts
        CHECK (delivery_attempts BETWEEN 0 AND 8),
    CONSTRAINT operation_finalizers_claim_tuple
        CHECK (
            (
                state = 'claimed'
                AND cloud_agents.is_valid_identifier(claim_holder_id)
                AND cloud_agents.is_valid_identifier(claim_incarnation)
                AND cloud_agents.is_valid_identifier(claim_token)
                AND claim_started_at IS NOT NULL
                AND claim_expires_at > claim_started_at
            )
            OR (
                state <> 'claimed'
                AND claim_holder_id IS NULL
                AND claim_incarnation IS NULL
                AND claim_token IS NULL
                AND claim_started_at IS NULL
                AND claim_expires_at IS NULL
            )
        ),
    CONSTRAINT operation_finalizers_retry_time
        CHECK (
            (state = 'retry_wait' AND next_attempt_at IS NOT NULL)
            OR (state <> 'retry_wait' AND next_attempt_at IS NULL)
        ),
    CONSTRAINT operation_finalizers_terminal
        CHECK (
            (
                state IN ('pending', 'claimed', 'retry_wait', 'unknown')
                AND terminal_at IS NULL
                AND stable_error_code IS NULL
            )
            OR (
                state = 'succeeded'
                AND terminal_at IS NOT NULL
                AND stable_error_code IS NULL
            )
            OR (
                state = 'dead_letter'
                AND terminal_at IS NOT NULL
                AND cloud_agents.is_valid_identifier(stable_error_code)
            )
        ),
    CONSTRAINT operation_finalizers_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT operation_finalizers_terminal_after_created
        CHECK (terminal_at IS NULL OR terminal_at >= created_at),
    CONSTRAINT operation_finalizers_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT operation_finalizers_operation_fk
        FOREIGN KEY (tenant_id, operation_id, operation_generation)
        REFERENCES cloud_agents.platform_operations
            (tenant_id, operation_id, operation_generation)
        ON UPDATE RESTRICT ON DELETE RESTRICT
);

CREATE INDEX operation_finalizers_claim_idx
    ON cloud_agents.operation_finalizers
    (tenant_id, state, next_attempt_at, claim_expires_at, operation_id, operation_generation);

CREATE INDEX operation_finalizers_operation_fk_idx
    ON cloud_agents.operation_finalizers
    (tenant_id, operation_id, operation_generation);

CREATE TABLE cloud_agents.idempotency_records (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    subject_digest text NOT NULL,
    registry_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    idempotency_key text NOT NULL,
    request_digest text NOT NULL,
    state text NOT NULL,
    operation_id text,
    operation_generation bigint,
    resource_kind text,
    resource_id text,
    resource_version bigint,
    stable_error_code text,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    expires_at timestamptz NOT NULL,
    terminal_at timestamptz,
    PRIMARY KEY (
        tenant_id,
        subject_digest,
        profile_id,
        profile_digest,
        idempotency_key
    ),
    CONSTRAINT idempotency_records_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT idempotency_records_subject_digest
        CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT idempotency_records_registry_digest
        CHECK (registry_digest = cloud_agents.coordination_registry_digest()),
    CONSTRAINT idempotency_records_profile
        CHECK (cloud_agents.coordination_profile_is_registered(profile_id, profile_digest)),
    CONSTRAINT idempotency_records_key
        CHECK (
            pg_catalog.char_length(idempotency_key) BETWEEN 16 AND 128
            AND idempotency_key ~ '^[A-Za-z0-9._~-]+$'
        ),
    CONSTRAINT idempotency_records_request_digest
        CHECK (request_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT idempotency_records_state CHECK (state IN ('pending', 'succeeded', 'failed')),
    CONSTRAINT idempotency_records_operation_reference
        CHECK (
            (operation_id IS NULL AND operation_generation IS NULL)
            OR (
                cloud_agents.is_valid_identifier(operation_id)
                AND operation_generation > 0
            )
        ),
    CONSTRAINT idempotency_records_terminal_envelope
        CHECK (
            (
                state = 'pending'
                AND resource_kind IS NULL
                AND resource_id IS NULL
                AND resource_version IS NULL
                AND stable_error_code IS NULL
                AND terminal_at IS NULL
            )
            OR (
                state = 'succeeded'
                AND cloud_agents.is_valid_identifier(resource_kind)
                AND cloud_agents.is_valid_identifier(resource_id)
                AND resource_version > 0
                AND stable_error_code IS NULL
                AND terminal_at IS NOT NULL
            )
            OR (
                state = 'failed'
                AND resource_kind IS NULL
                AND resource_id IS NULL
                AND resource_version IS NULL
                AND cloud_agents.is_valid_identifier(stable_error_code)
                AND terminal_at IS NOT NULL
            )
        ),
    CONSTRAINT idempotency_records_ttl
        CHECK (
            expires_at = created_at + pg_catalog.make_interval(
                secs => cloud_agents.coordination_profile_replay_ttl_seconds(profile_id, profile_digest)::integer
            )
        ),
    CONSTRAINT idempotency_records_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT idempotency_records_terminal_after_created
        CHECK (terminal_at IS NULL OR terminal_at >= created_at),
    CONSTRAINT idempotency_records_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT idempotency_records_operation_fk
        FOREIGN KEY (tenant_id, operation_id, operation_generation)
        REFERENCES cloud_agents.platform_operations
            (tenant_id, operation_id, operation_generation)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT idempotency_records_resource_fk
        FOREIGN KEY (tenant_id, resource_version, resource_kind, resource_id)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX idempotency_records_expiry_idx
    ON cloud_agents.idempotency_records
    (tenant_id, state, expires_at, profile_id, profile_digest, subject_digest);

CREATE INDEX idempotency_records_operation_fk_idx
    ON cloud_agents.idempotency_records
    (tenant_id, operation_id, operation_generation);

CREATE INDEX idempotency_records_tenant_fk_idx
    ON cloud_agents.idempotency_records (tenant_id, tenant_ref_id);

CREATE INDEX idempotency_records_resource_fk_idx
    ON cloud_agents.idempotency_records
    (tenant_id, resource_version, resource_kind, resource_id);

CREATE TABLE cloud_agents.outbox_events (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    event_id text NOT NULL,
    registry_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    event_class text NOT NULL,
    aggregate_kind text NOT NULL,
    aggregate_id text NOT NULL,
    aggregate_sequence bigint NOT NULL,
    resource_version bigint,
    generation bigint NOT NULL,
    operation_id text,
    operation_generation bigint,
    payload_digest text NOT NULL,
    state text NOT NULL,
    delivery_attempts integer NOT NULL,
    next_attempt_at timestamptz,
    claim_holder_id text,
    claim_incarnation text,
    claim_token text,
    claim_started_at timestamptz,
    claim_expires_at timestamptz,
    stable_error_code text,
    created_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    delivered_at timestamptz,
    dead_lettered_at timestamptz,
    PRIMARY KEY (tenant_id, event_id),
    CONSTRAINT outbox_events_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT outbox_events_event_id CHECK (cloud_agents.is_valid_identifier(event_id)),
    CONSTRAINT outbox_events_registry_digest
        CHECK (registry_digest = cloud_agents.coordination_registry_digest()),
    CONSTRAINT outbox_events_profile
        CHECK (
            cloud_agents.coordination_profile_is_registered(profile_id, profile_digest)
            AND event_class = cloud_agents.coordination_profile_outbox_class(profile_id, profile_digest)
        ),
    CONSTRAINT outbox_events_class CHECK (event_class IN ('resource_change', 'operation_effect')),
    CONSTRAINT outbox_events_aggregate_kind
        CHECK (cloud_agents.is_valid_identifier(aggregate_kind)),
    CONSTRAINT outbox_events_aggregate_id
        CHECK (cloud_agents.is_valid_identifier(aggregate_id)),
    CONSTRAINT outbox_events_aggregate_sequence CHECK (aggregate_sequence > 0),
    CONSTRAINT outbox_events_payload_digest
        CHECK (payload_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT outbox_events_state
        CHECK (state IN ('pending', 'claimed', 'retry_wait', 'delivered', 'dead_letter')),
    CONSTRAINT outbox_events_delivery_attempts CHECK (delivery_attempts BETWEEN 0 AND 8),
    CONSTRAINT outbox_events_class_shape
        CHECK (
            (
                event_class = 'resource_change'
                AND resource_version > 0
                AND generation = 0
                AND operation_id IS NULL
                AND operation_generation IS NULL
                AND aggregate_sequence = resource_version
            )
            OR (
                event_class = 'operation_effect'
                AND resource_version IS NULL
                AND generation > 0
                AND cloud_agents.is_valid_identifier(operation_id)
                AND operation_generation > 0
            )
        ),
    CONSTRAINT outbox_events_claim_tuple
        CHECK (
            (
                state = 'claimed'
                AND cloud_agents.is_valid_identifier(claim_holder_id)
                AND cloud_agents.is_valid_identifier(claim_incarnation)
                AND cloud_agents.is_valid_identifier(claim_token)
                AND claim_started_at IS NOT NULL
                AND claim_expires_at > claim_started_at
            )
            OR (
                state <> 'claimed'
                AND claim_holder_id IS NULL
                AND claim_incarnation IS NULL
                AND claim_token IS NULL
                AND claim_started_at IS NULL
                AND claim_expires_at IS NULL
            )
        ),
    CONSTRAINT outbox_events_retry_time
        CHECK (
            (state = 'retry_wait' AND next_attempt_at IS NOT NULL)
            OR (state <> 'retry_wait' AND next_attempt_at IS NULL)
        ),
    CONSTRAINT outbox_events_terminal
        CHECK (
            (
                state IN ('pending', 'claimed', 'retry_wait')
                AND delivered_at IS NULL
                AND dead_lettered_at IS NULL
                AND stable_error_code IS NULL
            )
            OR (
                state = 'delivered'
                AND delivered_at IS NOT NULL
                AND dead_lettered_at IS NULL
                AND stable_error_code IS NULL
            )
            OR (
                state = 'dead_letter'
                AND delivered_at IS NULL
                AND dead_lettered_at IS NOT NULL
                AND cloud_agents.is_valid_identifier(stable_error_code)
            )
        ),
    CONSTRAINT outbox_events_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT outbox_events_delivered_after_created
        CHECK (delivered_at IS NULL OR delivered_at >= created_at),
    CONSTRAINT outbox_events_dead_lettered_after_created
        CHECK (dead_lettered_at IS NULL OR dead_lettered_at >= created_at),
    CONSTRAINT outbox_events_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT outbox_events_operation_fk
        FOREIGN KEY (tenant_id, operation_id, operation_generation)
        REFERENCES cloud_agents.platform_operations
            (tenant_id, operation_id, operation_generation)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT outbox_events_resource_change_fk
        FOREIGN KEY (tenant_id, resource_version, aggregate_kind, aggregate_id)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE UNIQUE INDEX outbox_events_operation_effect_unique_idx
    ON cloud_agents.outbox_events
    (tenant_id, aggregate_kind, aggregate_id, generation, aggregate_sequence)
    WHERE event_class = 'operation_effect';

CREATE INDEX outbox_events_claim_idx
    ON cloud_agents.outbox_events
    (tenant_id, state, next_attempt_at, claim_expires_at, event_id);

CREATE INDEX outbox_events_operation_fk_idx
    ON cloud_agents.outbox_events
    (tenant_id, operation_id, operation_generation);

CREATE INDEX outbox_events_tenant_fk_idx
    ON cloud_agents.outbox_events (tenant_id, tenant_ref_id);

CREATE INDEX outbox_events_resource_change_fk_idx
    ON cloud_agents.outbox_events
    (tenant_id, resource_version, aggregate_kind, aggregate_id);

CREATE TABLE cloud_agents.coordination_audit_facts (
    tenant_id text NOT NULL,
    tenant_ref_id text NOT NULL,
    audit_fact_id text NOT NULL,
    registry_digest text NOT NULL,
    profile_id text NOT NULL,
    profile_digest text NOT NULL,
    subject_digest text NOT NULL,
    operation_id text,
    operation_generation bigint,
    attempt_number bigint,
    resource_kind text,
    resource_id text,
    resource_version bigint,
    transition text NOT NULL,
    outcome text,
    stable_error_code text,
    fencing_token bigint,
    database_timestamp timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (tenant_id, audit_fact_id),
    CONSTRAINT coordination_audit_facts_tenant_ref CHECK (tenant_id = tenant_ref_id),
    CONSTRAINT coordination_audit_facts_id
        CHECK (cloud_agents.is_valid_identifier(audit_fact_id)),
    CONSTRAINT coordination_audit_facts_registry_digest
        CHECK (registry_digest = cloud_agents.coordination_registry_digest()),
    CONSTRAINT coordination_audit_facts_profile
        CHECK (cloud_agents.coordination_profile_is_registered(profile_id, profile_digest)),
    CONSTRAINT coordination_audit_facts_subject_digest
        CHECK (subject_digest ~ '^sha256:[0-9a-f]{64}$'),
    CONSTRAINT coordination_audit_facts_operation_reference
        CHECK (
            (operation_id IS NULL AND operation_generation IS NULL)
            OR (
                cloud_agents.is_valid_identifier(operation_id)
                AND operation_generation > 0
            )
        ),
    CONSTRAINT coordination_audit_facts_attempt_reference
        CHECK (attempt_number IS NULL OR (operation_id IS NOT NULL AND attempt_number > 0)),
    CONSTRAINT coordination_audit_facts_resource_reference
        CHECK (
            (
                resource_kind IS NULL
                AND resource_id IS NULL
                AND resource_version IS NULL
            )
            OR (
                cloud_agents.is_valid_identifier(resource_kind)
                AND cloud_agents.is_valid_identifier(resource_id)
                AND resource_version > 0
            )
        ),
    CONSTRAINT coordination_audit_facts_transition
        CHECK (cloud_agents.is_valid_identifier(transition)),
    CONSTRAINT coordination_audit_facts_outcome
        CHECK (outcome IS NULL OR outcome IN ('pending', 'succeeded', 'failed', 'canceled', 'unknown')),
    CONSTRAINT coordination_audit_facts_error_code
        CHECK (stable_error_code IS NULL OR cloud_agents.is_valid_identifier(stable_error_code)),
    CONSTRAINT coordination_audit_facts_fencing_token
        CHECK (fencing_token IS NULL OR fencing_token > 0),
    CONSTRAINT coordination_audit_facts_tenant_fk
        FOREIGN KEY (tenant_id, tenant_ref_id)
        REFERENCES cloud_agents.platform_tenants (tenant_id, tenant_uid)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT coordination_audit_facts_operation_fk
        FOREIGN KEY (tenant_id, operation_id, operation_generation)
        REFERENCES cloud_agents.platform_operations
            (tenant_id, operation_id, operation_generation)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT coordination_audit_facts_attempt_fk
        FOREIGN KEY (tenant_id, operation_id, operation_generation, attempt_number)
        REFERENCES cloud_agents.operation_attempts
            (tenant_id, operation_id, operation_generation, attempt_number)
        ON UPDATE RESTRICT ON DELETE RESTRICT,
    CONSTRAINT coordination_audit_facts_resource_fk
        FOREIGN KEY (tenant_id, resource_version, resource_kind, resource_id)
        REFERENCES cloud_agents.resource_changes
            (tenant_id, resource_version, resource_kind, resource_uid)
        DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX coordination_audit_facts_subject_idx
    ON cloud_agents.coordination_audit_facts
    (tenant_id, subject_digest, database_timestamp, audit_fact_id);

CREATE INDEX coordination_audit_facts_operation_fk_idx
    ON cloud_agents.coordination_audit_facts
    (tenant_id, operation_id, operation_generation);

CREATE INDEX coordination_audit_facts_attempt_fk_idx
    ON cloud_agents.coordination_audit_facts
    (tenant_id, operation_id, operation_generation, attempt_number);

CREATE INDEX coordination_audit_facts_resource_fk_idx
    ON cloud_agents.coordination_audit_facts
    (tenant_id, resource_version, resource_kind, resource_id);

CREATE INDEX coordination_audit_facts_tenant_fk_idx
    ON cloud_agents.coordination_audit_facts (tenant_id, tenant_ref_id);

CREATE TABLE cloud_agents.leader_leases (
    leader_name text NOT NULL,
    holder_id text NOT NULL,
    holder_incarnation text NOT NULL,
    fencing_token bigint NOT NULL,
    lease_started_at timestamptz NOT NULL,
    lease_expires_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT pg_catalog.transaction_timestamp(),
    PRIMARY KEY (leader_name),
    CONSTRAINT leader_leases_name
        CHECK (leader_name IN ('coordination-reconciler', 'finalizer-reconciler', 'outbox-dispatcher')),
    CONSTRAINT leader_leases_holder_id CHECK (cloud_agents.is_valid_identifier(holder_id)),
    CONSTRAINT leader_leases_holder_incarnation
        CHECK (cloud_agents.is_valid_identifier(holder_incarnation)),
    CONSTRAINT leader_leases_fencing_token CHECK (fencing_token > 0),
    CONSTRAINT leader_leases_duration
        CHECK (
            lease_expires_at >= lease_started_at + interval '1 second'
            AND lease_expires_at <= lease_started_at + interval '60 seconds'
        ),
    CONSTRAINT leader_leases_updated_after_started CHECK (updated_at >= lease_started_at)
);

CREATE INDEX leader_leases_expiry_idx
    ON cloud_agents.leader_leases (lease_expires_at, leader_name, fencing_token);

-- The signed migration runner executes this transaction as
-- cloud_agents_migration_owner, so newly created objects already have the
-- exact required owner without redundant ALTER OWNER statements.
ALTER TABLE cloud_agents.platform_operations ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.platform_operations FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.operation_attempts ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.operation_attempts FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.terminal_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.terminal_receipts FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.operation_finalizers ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.operation_finalizers FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.idempotency_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.idempotency_records FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.outbox_events FORCE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.coordination_audit_facts ENABLE ROW LEVEL SECURITY;
ALTER TABLE cloud_agents.coordination_audit_facts FORCE ROW LEVEL SECURITY;

CREATE POLICY platform_operations_runtime_tenant
    ON cloud_agents.platform_operations
    FOR SELECT
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY platform_operations_migration_owner
    ON cloud_agents.platform_operations
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY operation_attempts_runtime_tenant
    ON cloud_agents.operation_attempts
    FOR SELECT
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY operation_attempts_migration_owner
    ON cloud_agents.operation_attempts
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY terminal_receipts_runtime_tenant
    ON cloud_agents.terminal_receipts
    FOR SELECT
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY terminal_receipts_migration_owner
    ON cloud_agents.terminal_receipts
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY operation_finalizers_runtime_tenant
    ON cloud_agents.operation_finalizers
    FOR SELECT
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY operation_finalizers_migration_owner
    ON cloud_agents.operation_finalizers
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY idempotency_records_runtime_tenant
    ON cloud_agents.idempotency_records
    FOR SELECT
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY idempotency_records_migration_owner
    ON cloud_agents.idempotency_records
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY outbox_events_runtime_tenant
    ON cloud_agents.outbox_events
    FOR SELECT
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY outbox_events_migration_owner
    ON cloud_agents.outbox_events
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

CREATE POLICY coordination_audit_facts_runtime_tenant
    ON cloud_agents.coordination_audit_facts
    FOR SELECT
    TO cloud_agents_runtime
    USING (tenant_id = cloud_agents.require_tenant_id());

CREATE POLICY coordination_audit_facts_migration_owner
    ON cloud_agents.coordination_audit_facts
    TO cloud_agents_migration_owner
    USING (true) WITH CHECK (true);

REVOKE ALL ON FUNCTION cloud_agents.coordination_registry_digest() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.coordination_state_machine_digest() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.coordination_policy_digest() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.coordination_profile_is_registered(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.coordination_profile_creates_operation(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.coordination_profile_outbox_class(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.coordination_profile_replay_ttl_seconds(text, text) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION cloud_agents.coordination_registry_digest()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.coordination_state_machine_digest()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.coordination_policy_digest()
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.coordination_profile_is_registered(text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.coordination_profile_creates_operation(text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.coordination_profile_outbox_class(text, text)
    TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.coordination_profile_replay_ttl_seconds(text, text)
    TO cloud_agents_runtime;

-- The migration-owner default ACL and PostgreSQL table defaults grant neither
-- PUBLIC nor cloud_agents_bootstrap_admin access to newly created tables.
-- Runtime receives only the explicit read surface below.
GRANT SELECT ON TABLE cloud_agents.platform_operations TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.operation_attempts TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.terminal_receipts TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.operation_finalizers TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.idempotency_records TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.outbox_events TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.coordination_audit_facts TO cloud_agents_runtime;
GRANT SELECT ON TABLE cloud_agents.leader_leases TO cloud_agents_runtime;
