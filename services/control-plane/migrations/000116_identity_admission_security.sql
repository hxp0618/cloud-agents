CREATE TABLE cloud_agents_identity.invitation_accept_attempts (
    code_digest bytea PRIMARY KEY
        REFERENCES cloud_agents_identity.invitations (code_digest)
        ON UPDATE RESTRICT ON DELETE CASCADE,
    attempts integer NOT NULL CHECK (attempts BETWEEN 1 AND 10),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (octet_length(code_digest) = 32)
);

CREATE TABLE cloud_agents_identity.invitation_accept_failures (
    ip_bucket bytea PRIMARY KEY CHECK (octet_length(ip_bucket) = 32),
    window_started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    failures integer NOT NULL DEFAULT 0 CHECK (failures BETWEEN 0 AND 100),
    locked_until timestamptz
);

CREATE INDEX invitation_accept_failures_window_idx
    ON cloud_agents_identity.invitation_accept_failures (window_started_at, ip_bucket);

ALTER TABLE cloud_agents_identity.invitation_accept_attempts OWNER TO cloud_agents_migration_owner;
ALTER TABLE cloud_agents_identity.invitation_accept_failures OWNER TO cloud_agents_migration_owner;

REVOKE ALL ON TABLE cloud_agents_identity.invitation_accept_attempts FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.invitation_accept_attempts FROM cloud_agents_identity_service;
REVOKE ALL ON TABLE cloud_agents_identity.invitation_accept_failures FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.invitation_accept_failures FROM cloud_agents_identity_service;

CREATE FUNCTION cloud_agents_identity.record_invitation_accept_attempt(
    p_code_digest bytea,
    p_ip_bucket bytea
) RETURNS TABLE (permitted boolean, rate_limited boolean)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    operation_time timestamptz := clock_timestamp();
    current_failures integer;
    current_lock timestamptz;
    proof_attempts integer;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_code_digest IS NULL OR octet_length(p_code_digest) <> 32
        OR p_ip_bucket IS NULL OR octet_length(p_ip_bucket) <> 32
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'invalid invitation acceptance attempt';
    END IF;

    DELETE FROM cloud_agents_identity.invitation_accept_failures AS expired
    WHERE expired.ip_bucket IN (
        SELECT candidate.ip_bucket
        FROM cloud_agents_identity.invitation_accept_failures AS candidate
        WHERE candidate.window_started_at <= operation_time - interval '30 minutes'
            AND (candidate.locked_until IS NULL OR candidate.locked_until <= operation_time)
        ORDER BY candidate.window_started_at, candidate.ip_bucket
        LIMIT 128
    );

    INSERT INTO cloud_agents_identity.invitation_accept_failures (ip_bucket)
        VALUES (p_ip_bucket)
        ON CONFLICT (ip_bucket) DO NOTHING;
    PERFORM 1
    FROM cloud_agents_identity.invitation_accept_failures AS failure
    WHERE failure.ip_bucket = p_ip_bucket
    FOR UPDATE;
    UPDATE cloud_agents_identity.invitation_accept_failures AS failure
    SET failures = 0,
        window_started_at = operation_time,
        locked_until = NULL
    WHERE failure.ip_bucket = p_ip_bucket
        AND failure.window_started_at <= operation_time - interval '15 minutes';
    UPDATE cloud_agents_identity.invitation_accept_failures AS failure
    SET failures = LEAST(failure.failures + 1, 100),
        locked_until = CASE
            WHEN failure.locked_until IS NOT NULL AND failure.locked_until > operation_time
                THEN failure.locked_until
            WHEN failure.failures + 1 >= 100
                THEN operation_time + interval '15 minutes'
            ELSE NULL
        END
    WHERE failure.ip_bucket = p_ip_bucket
    RETURNING failure.failures, failure.locked_until
        INTO current_failures, current_lock;

    IF current_failures >= 100 OR (current_lock IS NOT NULL AND current_lock > operation_time) THEN
        RETURN QUERY SELECT false, true;
        RETURN;
    END IF;

    INSERT INTO cloud_agents_identity.invitation_accept_attempts AS attempt (
        code_digest,
        attempts,
        updated_at
    )
    SELECT invitation.code_digest, 1, operation_time
    FROM cloud_agents_identity.invitations AS invitation
    WHERE invitation.code_digest = p_code_digest
    ON CONFLICT (code_digest) DO UPDATE
        SET attempts = attempt.attempts + 1,
            updated_at = operation_time
        WHERE attempt.attempts < 10
    RETURNING attempt.attempts INTO proof_attempts;

    RETURN QUERY SELECT proof_attempts IS NOT NULL, false;
END
$body$;

CREATE FUNCTION cloud_agents_identity.clear_invitation_accept_attempt(
    p_code_digest bytea,
    p_ip_bucket bytea
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    IF p_code_digest IS NULL OR octet_length(p_code_digest) <> 32
        OR p_ip_bucket IS NULL OR octet_length(p_ip_bucket) <> 32
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'invalid invitation acceptance cleanup';
    END IF;
    DELETE FROM cloud_agents_identity.invitation_accept_attempts AS attempt
        WHERE attempt.code_digest = p_code_digest;
    DELETE FROM cloud_agents_identity.invitation_accept_failures AS failure
        WHERE failure.ip_bucket = p_ip_bucket;
    RETURN true;
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.record_invitation_accept_attempt(bytea, bytea)
    FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.clear_invitation_accept_attempt(bytea, bytea)
    FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.record_invitation_accept_attempt(bytea, bytea)
    TO cloud_agents_identity_service;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.clear_invitation_accept_attempt(bytea, bytea)
    TO cloud_agents_identity_service;

CREATE FUNCTION cloud_agents.create_membership_v2(
    p_tenant_id text,
    p_expected_tenant_revision bigint,
    p_membership_uid text,
    p_membership_name text,
    p_subject_kind text,
    p_subject_issuer text,
    p_subject_value text,
    p_scope_level text,
    p_scope_uid text,
    p_expires_at timestamptz,
    p_audit_fact_uid text,
    p_reason_code text
)
RETURNS TABLE (resource_uid text, resource_version bigint, resource_state text)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    actor_principal text;
    mutation_at timestamptz;
    next_revision bigint;
    subject_digest text;
BEGIN
    actor_principal := cloud_agents.require_runtime_mutation_principal();
    mutation_at := pg_catalog.clock_timestamp();

    IF p_tenant_id IS DISTINCT FROM cloud_agents.require_tenant_id()
        OR p_subject_kind = 'user'
        OR NOT cloud_agents.is_valid_identifier(p_membership_uid)
        OR NOT cloud_agents.is_valid_identifier(p_membership_name)
        OR NOT cloud_agents.is_valid_identifier(p_audit_fact_uid)
        OR NOT cloud_agents.is_valid_identifier(p_reason_code)
        OR p_scope_level NOT IN ('tenant', 'organization', 'project')
        OR NOT cloud_agents.is_valid_identifier(p_scope_uid)
        OR (p_scope_level = 'tenant' AND p_scope_uid IS DISTINCT FROM p_tenant_id)
        OR (p_expires_at IS NOT NULL AND p_expires_at <= mutation_at)
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'direct human membership admission is unavailable';
    END IF;

    subject_digest := cloud_agents.subject_ref_digest(
        p_subject_kind,
        p_subject_issuer,
        p_subject_value
    );
    next_revision := cloud_agents.allocate_tenant_revision(
        p_tenant_id,
        p_expected_tenant_revision,
        mutation_at
    );

    INSERT INTO cloud_agents.resource_changes (
        tenant_id,
        tenant_uid,
        resource_version,
        resource_kind,
        resource_uid,
        change_kind,
        actor_database_principal,
        occurred_at
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        next_revision,
        'membership',
        p_membership_uid,
        'created',
        actor_principal,
        mutation_at
    );

    INSERT INTO cloud_agents.memberships (
        tenant_id,
        tenant_ref_id,
        membership_uid,
        membership_name,
        subject_kind,
        subject_issuer,
        subject_value,
        subject_digest,
        scope_level,
        scope_tenant_uid,
        scope_organization_uid,
        scope_project_uid,
        state,
        expires_at,
        resource_version,
        created_at,
        updated_at
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        p_membership_uid,
        p_membership_name,
        p_subject_kind,
        p_subject_issuer,
        p_subject_value,
        subject_digest,
        p_scope_level,
        CASE WHEN p_scope_level = 'tenant' THEN p_scope_uid END,
        CASE WHEN p_scope_level = 'organization' THEN p_scope_uid END,
        CASE WHEN p_scope_level = 'project' THEN p_scope_uid END,
        'active',
        p_expires_at,
        next_revision,
        mutation_at,
        mutation_at
    );

    INSERT INTO cloud_agents.audit_facts (
        tenant_id,
        tenant_uid,
        audit_fact_uid,
        resource_version,
        action,
        resource_kind,
        resource_uid,
        actor_database_principal,
        reason_code,
        occurred_at
    ) VALUES (
        p_tenant_id,
        p_tenant_id,
        p_audit_fact_uid,
        next_revision,
        'membership.create',
        'membership',
        p_membership_uid,
        actor_principal,
        p_reason_code,
        mutation_at
    );

    RETURN QUERY SELECT p_membership_uid, next_revision, 'active'::text;
END
$cloud_agents_function$;

REVOKE EXECUTE ON FUNCTION cloud_agents.create_membership(
    text, bigint, text, text, text, text, text, text, text, timestamptz, text, text
) FROM cloud_agents_runtime;
REVOKE ALL ON FUNCTION cloud_agents.create_membership_v2(
    text, bigint, text, text, text, text, text, text, text, timestamptz, text, text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.create_membership_v2(
    text, bigint, text, text, text, text, text, text, text, timestamptz, text, text
) TO cloud_agents_runtime;
