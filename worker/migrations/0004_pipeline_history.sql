-- A match's pipeline history: one row per status change, parse attempt,
-- reparse request and cleanup, kept after Workers Logs expire (7 days). seq
-- counts from 1 within a match.
CREATE TABLE match_events (
  match_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  ts INTEGER NOT NULL,           -- Unix ms
  kind TEXT NOT NULL,            -- status | attempt | reparse | cleanup
  msg TEXT NOT NULL,
  attempt INTEGER,
  build TEXT,
  parse_id TEXT,
  PRIMARY KEY (match_id, seq)
);

-- Container time used per month (UTC, "2026-10"), so the reparse cron can
-- stop before the plan's included allowance runs out.
CREATE TABLE container_usage (
  month TEXT PRIMARY KEY,
  ms INTEGER NOT NULL
);
