ALTER TABLE cloud_agents.remote_worker_enrollments
    ADD COLUMN incarnation_uid text CHECK (incarnation_uid IS NULL OR cloud_agents.is_valid_identifier(incarnation_uid)),
    ADD COLUMN csr_sha256 text CHECK (csr_sha256 IS NULL OR csr_sha256 ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN certificate_serial text CHECK (certificate_serial IS NULL OR certificate_serial ~ '^[0-9a-f]{1,32}$'),
    ADD COLUMN certificate_sha256 text CHECK (certificate_sha256 IS NULL OR certificate_sha256 ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN certificate_chain_pem text CHECK (certificate_chain_pem IS NULL OR pg_catalog.octet_length(certificate_chain_pem) BETWEEN 1 AND 32768),
    ADD COLUMN certificate_spiffe_id text,
    ADD COLUMN certificate_not_before timestamptz,
    ADD COLUMN certificate_not_after timestamptz,
    ADD COLUMN certificate_issued_by text CHECK (certificate_issued_by IS NULL OR certificate_issued_by ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN certificate_issue_idempotency_key text CHECK (certificate_issue_idempotency_key IS NULL OR certificate_issue_idempotency_key ~ '^[A-Za-z0-9._~-]{16,128}$'),
    ADD COLUMN certificate_issue_request_digest text CHECK (certificate_issue_request_digest IS NULL OR certificate_issue_request_digest ~ '^sha256:[0-9a-f]{64}$'),
    ADD COLUMN certificate_issue_request_id text CHECK (certificate_issue_request_id IS NULL OR cloud_agents.is_valid_identifier(certificate_issue_request_id)),
    ADD CONSTRAINT remote_worker_enrollments_certificate_serial_key UNIQUE (tenant_id, certificate_serial),
    ADD CONSTRAINT remote_worker_enrollments_certificate_sha256_key UNIQUE (tenant_id, certificate_sha256),
    ADD CONSTRAINT remote_worker_enrollments_certificate_state_check CHECK (
        (state = 'enrolled'
            AND incarnation_uid IS NOT NULL AND csr_sha256 IS NOT NULL
            AND certificate_serial IS NOT NULL AND certificate_sha256 IS NOT NULL
            AND certificate_chain_pem IS NOT NULL AND certificate_spiffe_id IS NOT NULL
            AND certificate_not_before IS NOT NULL AND certificate_not_after IS NOT NULL
            AND certificate_issued_by IS NOT NULL AND certificate_issue_idempotency_key IS NOT NULL
            AND certificate_issue_request_digest IS NOT NULL AND certificate_issue_request_id IS NOT NULL
            AND certificate_not_after > certificate_not_before)
        OR
        (state <> 'enrolled'
            AND incarnation_uid IS NULL AND csr_sha256 IS NULL
            AND certificate_serial IS NULL AND certificate_sha256 IS NULL
            AND certificate_chain_pem IS NULL AND certificate_spiffe_id IS NULL
            AND certificate_not_before IS NULL AND certificate_not_after IS NULL
            AND certificate_issued_by IS NULL AND certificate_issue_idempotency_key IS NULL
            AND certificate_issue_request_digest IS NULL AND certificate_issue_request_id IS NULL)
    );

ALTER TABLE cloud_agents.remote_worker_enrollment_activity
    DROP CONSTRAINT remote_worker_enrollment_activity_action_check;
ALTER TABLE cloud_agents.remote_worker_enrollment_activity
    ADD CONSTRAINT remote_worker_enrollment_activity_action_check CHECK (action IN (
        'remote-worker-enrollment.create', 'remote-worker-enrollment.claim-secret',
        'remote-worker-enrollment.issue-certificate', 'remote-worker-enrollment.revoke'
    ));

CREATE FUNCTION cloud_agents.authenticate_remote_worker_certificate_v1(
    p_tenant text, p_project text, p_enrollment text, p_expected_version bigint, p_secret_digest text
)
RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, cloud_agents
AS $$
DECLARE
    ignored text;
    existing cloud_agents.remote_worker_enrollments%ROWTYPE;
BEGIN
    ignored := cloud_agents.require_tenant_id();
    IF p_tenant IS DISTINCT FROM ignored OR NOT cloud_agents.is_valid_identifier(p_project)
       OR NOT cloud_agents.is_valid_identifier(p_enrollment) OR p_expected_version < 1
       OR p_secret_digest !~ '^sha256:[0-9a-f]{64}$' THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker enrollment authentication failed';
    END IF;
    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment;
    IF NOT FOUND OR existing.secret_digest IS DISTINCT FROM p_secret_digest
       OR existing.state NOT IN ('secret-issued', 'enrolled')
       OR existing.state = 'secret-issued' AND (existing.resource_version <> p_expected_version OR existing.expires_at <= clock_timestamp()) THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker enrollment authentication failed';
    END IF;
    RETURN true;
END;
$$;

CREATE FUNCTION cloud_agents.issue_remote_worker_certificate_v1(
    p_tenant text, p_project text, p_enrollment text, p_expected_version bigint,
    p_confirmed_enrollment text, p_secret_digest text, p_incarnation text,
    p_spiffe_id text, p_csr_digest text, p_certificate_serial text,
    p_certificate_digest text, p_certificate_chain text,
    p_certificate_not_before timestamptz, p_certificate_not_after timestamptz,
    p_subject text, p_key text, p_digest text, p_request text
)
RETURNS TABLE (
    enrollment_uid text, worker_uid text, worker_name text, state text,
    resource_version bigint, created_at timestamptz, updated_at timestamptz, expires_at timestamptz,
    secret_claimed_at timestamptz, enrolled_at timestamptz, revoked_at timestamptz,
    incarnation_uid text, certificate_spiffe_id text, certificate_sha256 text,
    certificate_chain_pem text, certificate_serial text,
    certificate_not_before timestamptz, certificate_not_after timestamptz
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
       OR p_secret_digest !~ '^sha256:[0-9a-f]{64}$' OR NOT cloud_agents.is_valid_identifier(p_incarnation)
       OR pg_catalog.left(p_spiffe_id, 9) <> 'spiffe://' OR pg_catalog.right(p_spiffe_id, pg_catalog.octet_length(identity_suffix)) <> identity_suffix
       OR pg_catalog.strpos(p_spiffe_id, '?') <> 0 OR pg_catalog.strpos(p_spiffe_id, '#') <> 0
       OR p_csr_digest !~ '^sha256:[0-9a-f]{64}$' OR p_certificate_serial !~ '^[0-9a-f]{1,32}$'
       OR p_certificate_digest !~ '^sha256:[0-9a-f]{64}$'
       OR pg_catalog.octet_length(p_certificate_chain) NOT BETWEEN 1 AND 32768
       OR pg_catalog.left(p_certificate_chain, 27) <> '-----BEGIN CERTIFICATE-----'
       OR p_certificate_not_before NOT BETWEEN mutation_at - interval '5 minutes' AND mutation_at + interval '1 minute'
       OR p_certificate_not_after <= mutation_at OR p_certificate_not_after > mutation_at + interval '20 minutes'
       OR p_certificate_not_after <= p_certificate_not_before OR p_subject !~ '^sha256:[0-9a-f]{64}$'
       OR p_key !~ '^[A-Za-z0-9._~-]{16,128}$' OR p_digest !~ '^sha256:[0-9a-f]{64}$'
       OR NOT cloud_agents.is_valid_identifier(p_request) THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'remote worker certificate input is invalid';
    END IF;
    SELECT enrollment.* INTO existing
    FROM cloud_agents.remote_worker_enrollments AS enrollment
    WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
      AND enrollment.enrollment_uid = p_enrollment
    FOR UPDATE;
    IF NOT FOUND OR existing.secret_digest IS DISTINCT FROM p_secret_digest THEN
        RAISE EXCEPTION USING ERRCODE = '28000', MESSAGE = 'remote worker enrollment authentication failed';
    END IF;
    IF existing.state = 'enrolled' THEN
        IF existing.certificate_issue_idempotency_key IS DISTINCT FROM p_key
           OR existing.certificate_issue_request_digest IS DISTINCT FROM p_digest
           OR existing.incarnation_uid IS DISTINCT FROM p_incarnation
           OR existing.csr_sha256 IS DISTINCT FROM p_csr_digest THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker certificate idempotency conflict';
        END IF;
    ELSE
        IF existing.state <> 'secret-issued' OR existing.resource_version <> p_expected_version
           OR existing.expires_at <= clock_timestamp() THEN
            RAISE EXCEPTION USING ERRCODE = '23505', MESSAGE = 'remote worker certificate enrollment is unavailable';
        END IF;
        UPDATE cloud_agents.remote_worker_enrollments AS enrollment
        SET state = 'enrolled', resource_version = enrollment.resource_version + 1,
            incarnation_uid = p_incarnation, csr_sha256 = p_csr_digest,
            certificate_serial = p_certificate_serial, certificate_sha256 = p_certificate_digest,
            certificate_chain_pem = p_certificate_chain, certificate_spiffe_id = p_spiffe_id,
            certificate_not_before = p_certificate_not_before, certificate_not_after = p_certificate_not_after,
            certificate_issued_by = p_subject, certificate_issue_idempotency_key = p_key,
            certificate_issue_request_digest = p_digest, certificate_issue_request_id = p_request,
            updated_at = mutation_at, enrolled_at = mutation_at
        WHERE enrollment.tenant_id = p_tenant AND enrollment.project_uid = p_project
          AND enrollment.enrollment_uid = p_enrollment
        RETURNING enrollment.* INTO existing;
        operation_value := 'op-' || pg_catalog.md5(p_tenant || '|' || p_project || '|issue-certificate|' || p_key);
        INSERT INTO cloud_agents.remote_worker_enrollment_activity (
            tenant_id, project_uid, enrollment_uid, event_uid, operation_uid, action,
            idempotency_key, request_id, request_digest, subject_digest,
            enrollment_resource_version, result, occurred_at
        ) VALUES (
            p_tenant, p_project, p_enrollment, operation_value || '-succeeded', operation_value,
            'remote-worker-enrollment.issue-certificate', p_key, p_request, p_digest, p_subject,
            existing.resource_version, 'succeeded', mutation_at
        );
    END IF;
    RETURN QUERY SELECT existing.enrollment_uid, existing.worker_uid, existing.worker_name, existing.state,
        existing.resource_version, existing.created_at, existing.updated_at, existing.expires_at,
        existing.secret_claimed_at, existing.enrolled_at, existing.revoked_at,
        existing.incarnation_uid, existing.certificate_spiffe_id, existing.certificate_sha256,
        existing.certificate_chain_pem, existing.certificate_serial,
        existing.certificate_not_before, existing.certificate_not_after;
END;
$$;

ALTER FUNCTION cloud_agents.issue_remote_worker_certificate_v1(text,text,text,bigint,text,text,text,text,text,text,text,text,timestamptz,timestamptz,text,text,text,text) OWNER TO cloud_agents_migration_owner;
ALTER FUNCTION cloud_agents.authenticate_remote_worker_certificate_v1(text,text,text,bigint,text) OWNER TO cloud_agents_migration_owner;
REVOKE ALL ON FUNCTION cloud_agents.authenticate_remote_worker_certificate_v1(text,text,text,bigint,text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents.issue_remote_worker_certificate_v1(text,text,text,bigint,text,text,text,text,text,text,text,text,timestamptz,timestamptz,text,text,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents.authenticate_remote_worker_certificate_v1(text,text,text,bigint,text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents.issue_remote_worker_certificate_v1(text,text,text,bigint,text,text,text,text,text,text,text,text,timestamptz,timestamptz,text,text,text,text) TO cloud_agents_runtime;
