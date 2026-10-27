-- Step 9: schema for the River job queue (LIBRARIES §2, OPERATIONS §5).
-- River's own tables are created by rivermigrate right after the goose
-- platform track (migrate.PlatformUp), as pos_owner. pos_jobs gets DML on
-- them through the default privileges below; nothing else can reach them.

-- +goose Up
CREATE SCHEMA IF NOT EXISTS river AUTHORIZATION pos_owner;
REVOKE ALL ON SCHEMA river FROM PUBLIC;
GRANT USAGE ON SCHEMA river TO pos_jobs;
ALTER DEFAULT PRIVILEGES FOR ROLE pos_owner IN SCHEMA river GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pos_jobs;
ALTER DEFAULT PRIVILEGES FOR ROLE pos_owner IN SCHEMA river GRANT USAGE, SELECT ON SEQUENCES TO pos_jobs;

-- +goose Down
DROP SCHEMA river CASCADE;
