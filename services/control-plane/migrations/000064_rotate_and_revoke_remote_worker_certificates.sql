ALTER TABLE cloud_agents.remote_worker_enrollments
    ADD COLUMN certificate_state text CHECK (certificate_state IS NULL OR certificate_state IN ('active', 'revoked')),
    ADD COLUMN previous_certificate_sha256 text CHECK (previous_certificate_sha256 IS NULL OR previous_certificate_sha256 ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN certificate_rotation_idempotency_key text CHECK (certificate_rotation_idempotency_key IS NULL OR certificate_rotation_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    ADD COLUMN certificate_rotation_request_digest text CHECK (certificate_rotation_request_digest IS NULL OR certificate_rotation_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN certificate_rotation_request_id text CHECK (certificate_rotation_request_id IS NULL OR cloud_agents.is_valid_identifier(certificate_rotation_request_id)),
    ADD COLUMN certificate_rotated_at timestamptz,
    ADD COLUMN certificate_revoked_by text CHECK (certificate_revoked_by IS NULL OR certificate_revoked_by ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN certificate_revoke_idempotency_key text CHECK (certificate_revoke_idempotency_key IS NULL OR certificate_revoke_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    ADD COLUMN certificate_revoke_request_digest text CHECK (certificate_revoke_request_digest IS NULL OR certificate_revoke_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN certificate_revoke_request_id text CHECK (certificate_revoke_request_id IS NULL OR cloud_agents.is_valid_identifier(certificate_revoke_request_id)),
    ADD COLUMN certificate_revoked_at timestamptz;

UPDATE cloud_agents.remote_worker_enrollments
SET certificate_state = 'active'
WHERE state = 'enrolled';

ALTER TABLE cloud_agents.remote_worker_enrollments
    ADD CONSTRAINT remote_worker_enrollments_certificate_lifecycle_check CHECK (
        (state = 'enrolled' AND certificate_state IN ('active', 'revoked')
            AND ((certificate_state = 'active'
                    AND certificate_revoked_by IS NULL AND certificate_revoke_idempotency_key IS NULL
                    AND certificate_revoke_request_digest IS NULL AND certificate_revoke_request_id IS NULL
                    AND certificate_revoked_at IS NULL)
                OR (certificate_state = 'revoked'
                    AND certificate_revoked_by IS NOT NULL AND certificate_revoke_idempotency_key IS NOT NULL
                    AND certificate_revoke_request_digest IS NOT NULL AND certificate_revoke_request_id IS NOT NULL
                    AND certificate_revoked_at IS NOT NULL AND certificate_revoked_at >= enrolled_at
                    AND certificate_revoked_at <= updated_at))
            AND ((previous_certificate_sha256 IS NULL
                    AND certificate_rotation_idempotency_key IS NULL
                    AND certificate_rotation_request_digest IS NULL
                    AND certificate_rotation_request_id IS NULL AND certificate_rotated_at IS NULL)
                OR (previous_certificate_sha256 IS NOT NULL
                    AND certificate_rotation_idempotency_key IS NOT NULL
                    AND certificate_rotation_request_digest IS NOT NULL
                    AND certificate_rotation_request_id IS NOT NULL AND certificate_rotated_at IS NOT NULL
                    AND certificate_rotated_at >= enrolled_at AND certificate_rotated_at <= updated_at)))
        OR (state <> 'enrolled' AND certificate_state IS NULL
            AND previous_certificate_sha256 IS NULL AND certificate_rotation_idempotency_key IS NULL
            AND certificate_rotation_request_digest IS NULL AND certificate_rotation_request_id IS NULL
            AND certificate_rotated_at IS NULL AND certificate_revoked_by IS NULL
            AND certificate_revoke_idempotency_key IS NULL AND certificate_revoke_request_digest IS NULL
            AND certificate_revoke_request_id IS NULL AND certificate_revoked_at IS NULL)
    );

CREATE FUNCTION cloud_agents.initialize_remote_worker_certificate_state_v1()
RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, cloud_agents
AS $$
BEGIN
    IF NEW.state = 'enrolled' AND OLD.state <> 'enrolled' AND NEW.certificate_state IS NULL THEN
        NEW.certificate_state := 'active';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER initialize_remote_worker_certificate_state_v1
BEFORE UPDATE OF state ON cloud_agents.remote_worker_enrollments
FOR EACH ROW EXECUTE FUNCTION cloud_agents.initialize_remote_worker_certificate_state_v1();

CREATE FUNCTION cloud_agents.authorize_remote_worker_certificate_rotation_v1(
    p_tenant text, p_project text, p_enrollment text, p_expected_version bigint,
    p_confirmed_enrollment text, p_peer_certificate_digest text,
    p_key text, p_digest text
)
RETURNS TABLE (
    needs_signing boolean,
    enrollment_uid text, worker_uid text, worker_name text, state text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, expires_at timestamptz,
    secret_claimed_at timestamptz, enrolled_at timestamptz, revoked_at timestamptz,
    incarnation_uid text, certificate_spiffe_id text, certificate_sha256 text,
    certificate_chain_pem text, certificate_serial text,
    certificate_not_before timestamptz, certificate_not_after timestamptz,
    certificate_state text, certificate_revoked_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    should_sign boolean;
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored
       OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR p_confirmed_enrollment IS DISTINCT FROM p_enrollment OR p_expected_version < 1
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$'
       OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;
    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment;
    IF NOT FOUND OR existing.state <> 'enrolled' OR existing.certificate_state <> 'active'
       OR existing.certificate_not_after <= clock_timestamp() THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;
    IF existing.resource_version = p_expected_version
       AND existing.certificate_sha256 = p_peer_certificate_digest THEN
        should_sign := true;
    ELSIF existing.resource_version = p_expected_version + 1
       AND existing.certificate_rotation_idempotency_key = p_key THEN
        IF existing.certificate_rotation_request_digest <> p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker certificate idempotency conflict';
        END IF;
        IF p_peer_certificate_digest NOT IN (existing.previous_certificate_sha256, existing.certificate_sha256) THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
        END IF;
        should_sign := false;
    ELSE
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;
    RETURN QUERY SELECT should_sign, existing.enrollment_uid, existing.worker_uid, existing.worker_name, existing.state,
        existing.resource_version, existing.created_at, existing.updated_at, existing.expires_at,
        existing.secret_claimed_at, existing.enrolled_at, existing.revoked_at,
        existing.incarnation_uid, existing.certificate_spiffe_id, existing.certificate_sha256,
        existing.certificate_chain_pem, existing.certificate_serial,
        existing.certificate_not_before, existing.certificate_not_after,
        existing.certificate_state, existing.certificate_revoked_at;
END;
$$;

CREATE FUNCTION cloud_agents.rotate_remote_worker_certificate_v1(
    p_tenant text, p_project text, p_enrollment text, p_expected_version bigint,
    p_confirmed_enrollment text, p_peer_certificate_digest text, p_incarnation text,
    p_spiffe_id text, p_csr_digest text, p_certificate_serial text,
    p_certificate_digest text, p_certificate_chain text,
    p_certificate_not_before timestamptz, p_certificate_not_after timestamptz,
    p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    enrollment_uid text, worker_uid text, worker_name text, state text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, expires_at timestamptz,
    secret_claimed_at timestamptz, enrolled_at timestamptz, revoked_at timestamptz,
    incarnation_uid text, certificate_spiffe_id text, certificate_sha256 text,
    certificate_chain_pem text, certificate_serial text,
    certificate_not_before timestamptz, certificate_not_after timestamptz,
    certificate_state text, certificate_revoked_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
    identity_suffix text := '/remote-worker/' || p_tenant || '/' || p_project || '/' || p_enrollment || '/' || p_incarnation;
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored
       OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR p_confirmed_enrollment IS DISTINCT FROM p_enrollment OR p_expected_version < 1
       OR p_peer_certificate_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_incarnation)
       OR pg_catalog.left(p_spiffe_id, 9) <> 'spiffe://' OR pg_catalog.right(p_spiffe_id, pg_catalog.octet_length(identity_suffix)) <> identity_suffix
       OR pg_catalog.strpos(p_spiffe_id, '?') <> 0 OR pg_catalog.strpos(p_spiffe_id, '#') <> 0
       OR p_csr_digest !~ '^sha256:[0-9a-f]{64}$' OR p_certificate_serial !~ '^[0-9a-f]{1,32}$'
       OR p_certificate_digest !~ '^sha256:[0-9a-f]{64}$' OR p_certificate_digest = p_peer_certificate_digest
       OR pg_catalog.octet_length(p_certificate_chain) NOT BETWEEN 1 AND 32768
       OR pg_catalog.left(p_certificate_chain, 27) <> '-----BEGIN CERTIFICATE-----'
       OR p_certificate_not_before NOT BETWEEN mutation_at - interval '5 minutes' AND mutation_at + interval '1 minute'
       OR p_certificate_not_after <= mutation_at OR p_certificate_not_after > mutation_at + interval '20 minutes'
       OR p_certificate_not_after <= p_certificate_not_before
       OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker certificate rotation input is invalid';
    END IF;
    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    FOR UPDATE;
    IF NOT FOUND OR existing.state <> 'enrolled' OR existing.certificate_state <> 'active' THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
    END IF;
    IF existing.resource_version = p_expected_version + 1
       AND existing.certificate_rotation_idempotency_key = p_key
       AND existing.certificate_rotation_request_digest <> p_digest THEN
        RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker certificate idempotency conflict';
    ELSIF existing.resource_version = p_expected_version + 1
       AND existing.certificate_rotation_idempotency_key = p_key
       AND existing.certificate_rotation_request_digest = p_digest
       AND existing.incarnation_uid = p_incarnation AND existing.csr_sha256 = p_csr_digest
       AND p_peer_certificate_digest IN (existing.previous_certificate_sha256, existing.certificate_sha256) THEN
        NULL;
    ELSE
        IF existing.resource_version <> p_expected_version
           OR existing.certificate_sha256 <> p_peer_certificate_digest
           OR existing.certificate_not_after <= clock_timestamp() THEN
            RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker certificate authentication failed';
        END IF;
        UPDATE cloud_agents.remote_worker_enrollments AS enrollment
        SET resource_version = enrollment.resource_version + 1,
            previous_certificate_sha256 = enrollment.certificate_sha256,
            incarnation_uid = p_incarnation, csr_sha256 = p_csr_digest,
            certificate_serial = p_certificate_serial, certificate_sha256 = p_certificate_digest,
            certificate_chain_pem = p_certificate_chain, certificate_spiffe_id = p_spiffe_id,
            certificate_not_before = p_certificate_not_before, certificate_not_after = p_certificate_not_after,
            certificate_rotation_idempotency_key = p_key,
            certificate_rotation_request_digest = p_digest,
            certificate_rotation_request_id = p_request,
            certificate_rotated_at = mutation_at, updated_at = mutation_at
        WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
          AND enrollment.enrollment_uid = p_enrollment
        RETURNING enrollment.* INTO existing;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|rotate-certificate|' || p_key);
        INSERT INTO cloud_agents.remote_worker_enrollment_activity (
            tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest,
            enrollment_resource_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, p_enrollment, operation_value || '-succeeded', operation_value,
            'remote-worker-enrollment.issue-certificate', p_key, p_request, p_digest,
            p_peer_certificate_digest, existing.resource_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.enrollment_uid, existing.worker_uid, existing.worker_name, existing.state,
        existing.resource_version, existing.created_at, existing.updated_at, existing.expires_at,
        existing.secret_claimed_at, existing.enrolled_at, existing.revoked_at,
        existing.incarnation_uid, existing.certificate_spiffe_id, existing.certificate_sha256,
        existing.certificate_chain_pem, existing.certificate_serial,
        existing.certificate_not_before, existing.certificate_not_after,
        existing.certificate_state, existing.certificate_revoked_at;
END;
$$;

CREATE FUNCTION cloud_agents.revoke_remote_worker_enrollment_or_certificate_v1(
    p_tenant text, p_project text, p_enrollment text, p_expected_version bigint,
    p_confirmed_enrollment text, p_subject text, p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    enrollment_uid text, worker_uid text, worker_name text, state text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, expires_at timestamptz,
    secret_claimed_at timestamptz, enrolled_at timestamptz, revoked_at timestamptz,
    incarnation_uid text, certificate_spiffe_id text, certificate_sha256 text,
    certificate_chain_pem text, certificate_serial text,
    certificate_not_before timestamptz, certificate_not_after timestamptz,
    certificate_state text, certificate_revoked_at timestamptz
)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
    mutation_at timestamptz := transaction_timestamp();
    operation_value text;
BEGIN
    ignored := cloud_agents.require_runtime_mutation_principal();
    IF p_tenant IS DISTINCT FROM cloud_agents.require_tenant_id()
       OR NOT cloud_agents.is_valid_identifier(p_project) OR NOT cloud_agents.is_valid_identifier(p_enrollment)
       OR p_confirmed_enrollment IS DISTINCT FROM p_enrollment OR p_expected_version < 1
       OR p_subject !~ '^sha256:[0-9a-f]{64}$' OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$'
       OR p_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker revoke input is invalid';
    END IF;
    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE = '23503', MESSAGE = 'remote worker enrollment was not found'; END IF;
    IF existing.state = 'revoked' THEN
        IF existing.revoked_by <> p_subject OR existing.revoke_idempotency_key <> p_key
           OR existing.revoke_request_digest <> p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker enrollment revoke conflict';
        END IF;
    ELSIF existing.state = 'enrolled' AND existing.certificate_state = 'revoked' THEN
        IF existing.certificate_revoked_by <> p_subject OR existing.certificate_revoke_idempotency_key <> p_key
           OR existing.certificate_revoke_request_digest <> p_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker certificate revoke conflict';
        END IF;
    ELSIF existing.state = 'enrolled' THEN
        IF existing.certificate_state <> 'active' OR existing.resource_version <> p_expected_version THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker certificate revoke conflict';
        END IF;
        UPDATE cloud_agents.remote_worker_enrollments AS enrollment
        SET certificate_state = 'revoked', resource_version = enrollment.resource_version + 1,
            certificate_revoked_by = p_subject, certificate_revoke_idempotency_key = p_key,
            certificate_revoke_request_digest = p_digest, certificate_revoke_request_id = p_request,
            certificate_revoked_at = mutation_at, updated_at = mutation_at
        WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
          AND enrollment.enrollment_uid = p_enrollment
        RETURNING enrollment.* INTO existing;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|revoke-certificate|' || p_key);
    ELSE
        IF existing.state NOT IN ('pending', 'secret-issued') OR existing.resource_version <> p_expected_version THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker enrollment revoke conflict';
        END IF;
        UPDATE cloud_agents.remote_worker_enrollments AS enrollment
        SET state = 'revoked', resource_version = enrollment.resource_version + 1,
            revoked_by = p_subject, revoke_idempotency_key = p_key, revoke_request_digest = p_digest,
            revoke_request_id = p_request, updated_at = mutation_at, revoked_at = mutation_at
        WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
          AND enrollment.enrollment_uid = p_enrollment
        RETURNING enrollment.* INTO existing;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|revoke|' || p_key);
    END IF;
    IF operation_value IS NOT NULL THEN
        INSERT INTO cloud_agents.remote_worker_enrollment_activity (
            tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest,
            enrollment_resource_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, p_enrollment, operation_value || '-succeeded', operation_value,
            'remote-worker-enrollment.revoke', p_key, p_request, p_digest, p_subject,
            existing.resource_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.enrollment_uid, existing.worker_uid, existing.worker_name, existing.state,
        existing.resource_version, existing.created_at, existing.updated_at, existing.expires_at,
        existing.secret_claimed_at, existing.enrolled_at, existing.revoked_at,
        existing.incarnation_uid, existing.certificate_spiffe_id, existing.certificate_sha256,
        existing.certificate_chain_pem, existing.certificate_serial,
        existing.certificate_not_before, existing.certificate_not_after,
        existing.certificate_state, existing.certificate_revoked_at;
END;
$$;

ALTER FUNCTION cloud_agents.initialize_remote_worker_certificate_state_v1() OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.authorize_remote_worker_certificate_rotation_v1(text,text,text,bigint,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.rotate_remote_worker_certificate_v1(text,text,text,bigint,text,text,text,text,text,text,text,text,timestamptz,timestamptz,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.revoke_remote_worker_enrollment_or_certificate_v1(text,text,text,bigint,text,text,text,text,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.initialize_remote_worker_certificate_state_v1() FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.authorize_remote_worker_certificate_rotation_v1(text,text,text,bigint,text,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.rotate_remote_worker_certificate_v1(text,text,text,bigint,text,text,text,text,text,text,text,text,timestamptz,timestamptz,text,text,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.revoke_remote_worker_enrollment_or_certificate_v1(text,text,text,bigint,text,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.authorize_remote_worker_certificate_rotation_v1(text,text,text,bigint,text,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.rotate_remote_worker_certificate_v1(text,text,text,bigint,text,text,text,text,text,text,text,text,timestamptz,timestamptz,text,text,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.revoke_remote_worker_enrollment_or_certificate_v1(text,text,text,bigint,text,text,text,text,text) TO cloud_agents_runtime;
