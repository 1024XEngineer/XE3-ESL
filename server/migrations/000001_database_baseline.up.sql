BEGIN;

-- This intentionally creates no application objects. Applying it proves that
-- the migration history, advisory lock, and version tracking are operational.
SELECT 1;

COMMIT;
