-- Case-insensitive email uniqueness enforced by the column type, so no write
-- path can register a second User@x.com by skipping a lowercase step (ADR-006).
CREATE EXTENSION IF NOT EXISTS citext;

-- Shared by every table with an updated_at. A trigger keeps the column honest
-- even when a hand-written UPDATE forgets it, which hand-written SQL will.
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email         citext      NOT NULL UNIQUE,
    password_hash text        NOT NULL,
    full_name     text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT users_email_not_blank CHECK (btrim(email::text) <> ''),
    CONSTRAINT users_full_name_not_blank CHECK (btrim(full_name) <> ''),
    -- bcrypt output is always 60 characters in this shape. The constraint is
    -- here to catch a plaintext password reaching the column, which is the one
    -- bug in this area worth a database-level tripwire.
    CONSTRAINT users_password_hash_is_bcrypt CHECK (password_hash ~ '^\$2[aby]\$[0-9]{2}\$.{53}$')
);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();
