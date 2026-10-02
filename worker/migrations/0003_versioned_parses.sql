-- v1.1: each parse writes under its own prefix, parsed/{id}/{parse_id}/, and
-- the match points at the live one. NULL means the v1 layout, written
-- straight under parsed/{id}/.
ALTER TABLE matches ADD COLUMN live_parse_id TEXT;
-- The parser build of the latest parse attempt, so the reparse cron tries each
-- match once per build.
ALTER TABLE matches ADD COLUMN attempted_build TEXT;
-- A failed reparse leaves the match ready on its live parse and records why
-- here, with the build that failed.
ALTER TABLE matches ADD COLUMN reparse_error TEXT;
ALTER TABLE matches ADD COLUMN reparse_build TEXT;
-- Unix ms of the last status change, for the stuck-match sweeper.
ALTER TABLE matches ADD COLUMN status_changed_at INTEGER;
UPDATE matches SET status_changed_at = COALESCE(parsed_at, created_at);
