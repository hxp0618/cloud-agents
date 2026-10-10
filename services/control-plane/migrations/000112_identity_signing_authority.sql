CREATE TABLE cloud_agents_identity.signing_keys (
    key_id text PRIMARY KEY CHECK (key_id ~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'),
    modulus text NOT NULL CHECK (length(modulus) BETWEEN 342 AND 683 AND modulus ~ '^[A-Za-z0-9_-]+$'),
    not_before bigint NOT NULL CHECK (not_before BETWEEN 0 AND 253402300799),
    not_after bigint NOT NULL CHECK (not_after BETWEEN 1 AND 253402300799 AND not_after > not_before),
    enabled boolean NOT NULL
);

CREATE TABLE cloud_agents_identity.signing_authority (
    singleton boolean PRIMARY KEY CHECK (singleton),
    current_key_id text NOT NULL REFERENCES cloud_agents_identity.signing_keys (key_id),
    revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
    security_epoch bigint NOT NULL CHECK (security_epoch BETWEEN 1 AND 9007199254740991),
    not_before bigint NOT NULL CHECK (not_before BETWEEN 0 AND 253402300799),
    expires_at bigint NOT NULL CHECK (expires_at BETWEEN 1 AND 253402300799
        AND expires_at > not_before AND expires_at <= not_before + 86400)
);

REVOKE ALL ON TABLE cloud_agents_identity.signing_keys FROM PUBLIC;
REVOKE ALL ON TABLE cloud_agents_identity.signing_authority FROM PUBLIC;

CREATE FUNCTION cloud_agents_identity.initialize_signing_authority(
    p_issuer text, p_setup_digest bytea, p_key_id text, p_modulus text,
    p_not_before bigint, p_not_after bigint
) RETURNS boolean
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    realm_row cloud_agents_identity.realm%ROWTYPE;
    key_row cloud_agents_identity.signing_keys%ROWTYPE;
    operation_time bigint := floor(extract(epoch FROM clock_timestamp()))::bigint;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_bootstrap_admin');
    SELECT * INTO STRICT realm_row FROM cloud_agents_identity.realm WHERE singleton FOR UPDATE;
    IF p_issuer IS DISTINCT FROM realm_row.issuer
        OR p_setup_digest IS DISTINCT FROM realm_row.setup_digest
        OR p_key_id IS NULL OR p_key_id !~ '^[A-Za-z0-9]([A-Za-z0-9._~-]{0,126}[A-Za-z0-9])?$'
        OR p_modulus IS NULL OR length(p_modulus) NOT BETWEEN 342 AND 683 OR p_modulus !~ '^[A-Za-z0-9_-]+$'
        OR p_not_before IS NULL OR p_not_before NOT BETWEEN 0 AND 253402300799
        OR p_not_after IS NULL OR p_not_after NOT BETWEEN 1 AND 253402300799
        OR p_not_after <= p_not_before
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'identity signing installation is invalid';
    END IF;

    IF EXISTS (SELECT 1 FROM cloud_agents_identity.signing_authority WHERE singleton) THEN
        SELECT * INTO key_row FROM cloud_agents_identity.signing_keys WHERE key_id = p_key_id;
        IF NOT FOUND OR key_row.modulus IS DISTINCT FROM p_modulus
            OR key_row.not_before IS DISTINCT FROM p_not_before OR key_row.not_after IS DISTINCT FROM p_not_after
        THEN
            RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'identity signing installation conflicts with existing authority';
        END IF;
        RETURN false;
    END IF;
    IF p_not_before > operation_time OR p_not_after <= operation_time
        OR EXISTS (SELECT 1 FROM cloud_agents_identity.signing_keys)
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'identity signing key is not usable';
    END IF;
    INSERT INTO cloud_agents_identity.signing_keys (key_id, modulus, not_before, not_after, enabled)
        VALUES (p_key_id, p_modulus, p_not_before, p_not_after, true);
    INSERT INTO cloud_agents_identity.signing_authority (singleton, current_key_id, revision, security_epoch, not_before, expires_at)
        VALUES (true, p_key_id, 1, 1, operation_time, operation_time + 86400);
    RETURN true;
END
$body$;

CREATE FUNCTION cloud_agents_identity.read_signing_authority(p_issuer text, p_key_id text, p_modulus text)
RETURNS TABLE (
    revision bigint, security_epoch bigint, not_before bigint, expires_at bigint,
    key_id text, modulus text, key_not_before bigint, key_not_after bigint, enabled boolean, current_key boolean
)
LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, cloud_agents_identity
AS $body$
DECLARE
    authority_row cloud_agents_identity.signing_authority%ROWTYPE;
    current_key_row cloud_agents_identity.signing_keys%ROWTYPE;
    operation_time bigint;
BEGIN
    PERFORM cloud_agents_identity.require_principal('cloud_agents_identity_service');
    SELECT * INTO STRICT authority_row FROM cloud_agents_identity.signing_authority WHERE singleton FOR UPDATE;
    operation_time := floor(extract(epoch FROM clock_timestamp()))::bigint;
    SELECT * INTO STRICT current_key_row FROM cloud_agents_identity.signing_keys
        WHERE signing_keys.key_id = authority_row.current_key_id;
    IF NOT EXISTS (SELECT 1 FROM cloud_agents_identity.realm WHERE issuer = p_issuer)
        OR p_key_id IS DISTINCT FROM current_key_row.key_id
        OR p_modulus IS DISTINCT FROM current_key_row.modulus
        OR NOT current_key_row.enabled
        OR operation_time < current_key_row.not_before OR operation_time >= current_key_row.not_after
        OR operation_time < authority_row.not_before
        OR (SELECT count(*) FROM cloud_agents_identity.signing_keys) NOT BETWEEN 1 AND 32
    THEN
        RAISE EXCEPTION USING ERRCODE = '22023', MESSAGE = 'identity signing authority is not usable';
    END IF;
    IF operation_time >= authority_row.not_before + 21600 THEN
        UPDATE cloud_agents_identity.signing_authority AS authority
            SET revision = authority_row.revision + 1, not_before = operation_time, expires_at = operation_time + 86400
            WHERE singleton
            RETURNING * INTO authority_row;
    END IF;
    RETURN QUERY SELECT authority_row.revision, authority_row.security_epoch, authority_row.not_before, authority_row.expires_at,
        key_row.key_id, key_row.modulus, key_row.not_before, key_row.not_after, key_row.enabled,
        key_row.key_id = authority_row.current_key_id
        FROM cloud_agents_identity.signing_keys AS key_row ORDER BY key_row.key_id;
END
$body$;

REVOKE ALL ON FUNCTION cloud_agents_identity.initialize_signing_authority(text, bytea, text, text, bigint, bigint) FROM PUBLIC;
REVOKE ALL ON FUNCTION cloud_agents_identity.read_signing_authority(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.initialize_signing_authority(text, bytea, text, text, bigint, bigint) TO cloud_agents_bootstrap_admin;
GRANT EXECUTE ON FUNCTION cloud_agents_identity.read_signing_authority(text, text, text) TO cloud_agents_identity_service;
