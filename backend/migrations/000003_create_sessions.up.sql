-- One row per login. The session row, not the token, is the source of truth for
-- whether a request is authenticated, which is what makes revocation immediate
-- while access tokens stay long-lived (ADR-007).
CREATE TABLE sessions (
    id                    uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Identifier carried in the access token's jti claim. Stored in the clear:
    -- it identifies, it does not authorize — the signature does that (ADR-008).
    access_jti            text        NOT NULL,
    -- SHA-256 of the opaque refresh token. The token itself is the credential,
    -- so a database leak must not hand over usable refresh tokens.
    refresh_hash          bytea       NOT NULL,
    -- One generation of history. A token matching this is a genuine replay of
    -- an already-rotated token, and revokes every session for the user (ADR-009).
    previous_refresh_hash bytea,
    user_agent            text        NOT NULL DEFAULT '',
    expires_at            timestamptz NOT NULL,
    revoked_at            timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT sessions_access_jti_not_blank CHECK (btrim(access_jti) <> ''),
    CONSTRAINT sessions_refresh_hash_is_sha256 CHECK (length(refresh_hash) = 32),
    CONSTRAINT sessions_previous_refresh_hash_is_sha256
        CHECK (previous_refresh_hash IS NULL OR length(previous_refresh_hash) = 32)
);

-- Both hot lookups: the JWT middleware finds the session by jti on every
-- authenticated request, refresh finds it by the hash of the presented token.
-- Rotation overwrites in place, so each value identifies at most one session.
CREATE UNIQUE INDEX sessions_access_jti_key ON sessions (access_jti);
CREATE UNIQUE INDEX sessions_refresh_hash_key ON sessions (refresh_hash);

-- Reuse detection only ever asks about rows that have a previous generation.
CREATE INDEX sessions_previous_refresh_hash_idx
    ON sessions (previous_refresh_hash)
    WHERE previous_refresh_hash IS NOT NULL;

-- Revoke-all on password change, and the Step 2 active-sessions view.
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- The background reaper sweeps by expiry.
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

CREATE TRIGGER sessions_set_updated_at
    BEFORE UPDATE ON sessions
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();
