DROP TABLE IF EXISTS users;

-- Both are introduced by this migration, so reversing it removes them. Later
-- migrations depend on the function and are rolled back before this runs.
DROP FUNCTION IF EXISTS set_updated_at();
DROP EXTENSION IF EXISTS citext;
