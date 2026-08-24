CREATE OR REPLACE FUNCTION cloud_agents.subject_ref_digest(
    p_subject_kind text,
    p_subject_issuer text,
    p_subject_value text
)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE
STRICT
SET search_path = pg_catalog, cloud_agents
AS $cloud_agents_function$
DECLARE
    canonical_subject text;
    issuer_utf8 bytea;
    issuer_byte_index integer;
    issuer_byte integer;
    issuer_byte_count integer;
BEGIN
    IF p_subject_kind NOT IN ('user', 'serviceAccount', 'workload')
        OR pg_catalog.char_length(p_subject_issuer) NOT BETWEEN 1 AND 512
        OR p_subject_issuer !~ '^[A-Za-z][A-Za-z0-9+.-]*:'
        OR pg_catalog.char_length(p_subject_value) NOT BETWEEN 1 AND 256
    THEN
        RAISE EXCEPTION USING
            ERRCODE = '22023',
            MESSAGE = 'subject reference is outside the closed mutation profile';
    END IF;

    issuer_utf8 := pg_catalog.convert_to(p_subject_issuer, 'UTF8');
    issuer_byte_count := pg_catalog.octet_length(issuer_utf8);
    issuer_byte_index := 0;
    WHILE issuer_byte_index < issuer_byte_count LOOP
        issuer_byte := pg_catalog.get_byte(issuer_utf8, issuer_byte_index);
        IF issuer_byte < 32 OR issuer_byte = 127 THEN
            RAISE EXCEPTION USING
                ERRCODE = '22023',
                MESSAGE = 'subject reference is outside the closed mutation profile';
        END IF;
        IF issuer_byte = 37 THEN
            IF issuer_byte_index + 2 >= issuer_byte_count THEN
                RAISE EXCEPTION USING
                    ERRCODE = '22023',
                    MESSAGE = 'subject reference is outside the closed mutation profile';
            END IF;
            IF NOT (
                    pg_catalog.get_byte(issuer_utf8, issuer_byte_index + 1) BETWEEN 48 AND 57
                    OR pg_catalog.get_byte(issuer_utf8, issuer_byte_index + 1) BETWEEN 65 AND 70
                    OR pg_catalog.get_byte(issuer_utf8, issuer_byte_index + 1) BETWEEN 97 AND 102
                )
                OR NOT (
                    pg_catalog.get_byte(issuer_utf8, issuer_byte_index + 2) BETWEEN 48 AND 57
                    OR pg_catalog.get_byte(issuer_utf8, issuer_byte_index + 2) BETWEEN 65 AND 70
                    OR pg_catalog.get_byte(issuer_utf8, issuer_byte_index + 2) BETWEEN 97 AND 102
                )
            THEN
                RAISE EXCEPTION USING
                    ERRCODE = '22023',
                    MESSAGE = 'subject reference is outside the closed mutation profile';
            END IF;
            issuer_byte_index := issuer_byte_index + 2;
        END IF;
        issuer_byte_index := issuer_byte_index + 1;
    END LOOP;

    canonical_subject :=
        '{"issuer":' || pg_catalog.to_json(p_subject_issuer)::text
        || ',"kind":' || pg_catalog.to_json(p_subject_kind)::text
        || ',"subject":' || pg_catalog.to_json(p_subject_value)::text
        || '}';

    RETURN 'sha256:' || pg_catalog.encode(
        pg_catalog.sha256(pg_catalog.convert_to(canonical_subject, 'UTF8')),
        'hex'
    );
END
$cloud_agents_function$;
