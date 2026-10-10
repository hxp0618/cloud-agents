CREATE TABLE cloud_agents_identity.trust_checkpoints (
    issuer text PRIMARY KEY
        CHECK (
            octet_length(issuer) BETWEEN 1 AND 2048
            AND issuer = pg_catalog.btrim(issuer)
            AND issuer LIKE 'https://_%'
            AND pg_catalog.strpos(issuer, E'\n') = 0
            AND pg_catalog.strpos(issuer, E'\r') = 0
        ),
    authority_digest text NOT NULL
        CHECK (authority_digest ~ '^sha256:[0-9a-f]{64}$'),
    state_bytes bytea NOT NULL
        CHECK (octet_length(state_bytes) BETWEEN 1 AND 1048576),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

REVOKE ALL ON TABLE cloud_agents_identity.trust_checkpoints FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.trust_checkpoints FROM cloud_agents_identity_service;

CREATE FUNCTION cloud_agents_identity.load_trust_checkpoint(p_issuer text)
RETURNS TABLE (authority_digest text, state_bytes bytea)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_issuer IS NULL
        OR octet_length(p_issuer) NOT BETWEEN 1 AND 2048
        OR p_issuer IS DISTINCT FROM pg_catalog.btrim(p_issuer)
        OR p_issuer NOT LIKE 'https://_%'
        OR pg_catalog.strpos(p_issuer, E'\n') <> 0
        OR pg_catalog.strpos(p_issuer, E'\r') <> 0
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'identity trust issuer is invalid';
    END IF;
    RETURN QUERY
        SELECT checkpoint.authority_digest, checkpoint.state_bytes
        FROM cloud_agents_identity.trust_checkpoints AS checkpoint
        WHERE checkpoint.issuer = p_issuer;
END
$body$;

CREATE FUNCTION cloud_agents_identity.compare_trust_checkpoint(
    p_issuer text,
    p_expected_digest text,
    p_authority_digest text,
    p_state_bytes bytea
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    changed integer;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_runtime');
    IF p_issuer IS NULL
        OR octet_length(p_issuer) NOT BETWEEN 1 AND 2048
        OR p_issuer IS DISTINCT FROM pg_catalog.btrim(p_issuer)
        OR p_issuer NOT LIKE 'https://_%'
        OR pg_catalog.strpos(p_issuer, E'\n') <> 0
        OR pg_catalog.strpos(p_issuer, E'\r') <> 0
        OR p_expected_digest IS NULL
        OR (p_expected_digest <> '' AND p_expected_digest !~ '^sha256:[0-9a-f]{64}$')
        OR p_authority_digest IS NULL
        OR p_authority_digest !~ '^sha256:[0-9a-f]{64}$'
        OR p_state_bytes IS NULL
        OR octet_length(p_state_bytes) NOT BETWEEN 1 AND 1048576
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'identity trust checkpoint input is invalid';
    END IF;

    IF p_expected_digest = '' THEN
        INSERT INTO cloud_agents_identity.trust_checkpoints (
            issuer, authority_digest, state_bytes
        ) VALUES (
            p_issuer, p_authority_digest, p_state_bytes
        )
        ON CONFLICT (issuer) DO NOTHING;
    ELSE
        UPDATE cloud_agents_identity.trust_checkpoints AS checkpoint
        SET authority_digest = p_authority_digest,
            state_bytes = p_state_bytes,
            updated_at = clock_timestamp()
        WHERE checkpoint.issuer = p_issuer
            AND checkpoint.authority_digest = p_expected_digest;
    END IF;
    GET DIAGNOSTICS changed = ROW_COUNT;
    RETURN changed = 1;
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.load_trust_checkpoint(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.compare_trust_checkpoint(text, text, text, bytea) FROM PUBLIC;
GRANT USAGE ON SCHEMA cloud_agents_identity TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.load_trust_checkpoint(text) TO cloud_agents_runtime;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.compare_trust_checkpoint(text, text, text, bytea) TO cloud_agents_runtime;
