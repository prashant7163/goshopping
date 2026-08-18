CREATE TABLE user_addresses (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    street      text        NOT NULL,
    city        text        NOT NULL,
    state       text        NOT NULL,
    postal_code text        NOT NULL,
    country     text        NOT NULL,
    is_default  boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    -- state has no not-blank check: plenty of countries have no such
    -- subdivision, and an empty string is the honest answer there.
    CONSTRAINT user_addresses_street_not_blank CHECK (btrim(street) <> ''),
    CONSTRAINT user_addresses_city_not_blank CHECK (btrim(city) <> ''),
    CONSTRAINT user_addresses_postal_code_not_blank CHECK (btrim(postal_code) <> ''),
    CONSTRAINT user_addresses_country_not_blank CHECK (btrim(country) <> '')
);

-- Every address query is scoped to one user.
CREATE INDEX user_addresses_user_id_idx ON user_addresses (user_id);

-- At most one default per user, as a database invariant rather than a rule the
-- application remembers to apply under concurrent writes (ADR-013). Partial, so
-- the many non-default rows per user stay unconstrained.
CREATE UNIQUE INDEX user_addresses_one_default_per_user_idx
    ON user_addresses (user_id)
    WHERE is_default;

CREATE TRIGGER user_addresses_set_updated_at
    BEFORE UPDATE ON user_addresses
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();
