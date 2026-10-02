-- Match Pulse (v1.2): Jev's input tokens per month (UTC), for the Worker's own
-- cap under the AI Gateway credit, and one self-grading row per Pulse: how
-- well its win-probability line called the real round winners.
CREATE TABLE ai_usage (
  month TEXT PRIMARY KEY,
  calls INTEGER NOT NULL,
  in_tokens INTEGER NOT NULL
);

CREATE TABLE pulse_quality (
  match_id TEXT NOT NULL,
  parse_id TEXT NOT NULL,
  model TEXT NOT NULL,
  state_version INTEGER NOT NULL,
  moments INTEGER NOT NULL,
  brier REAL NOT NULL,
  logloss REAL NOT NULL,
  rounds INTEGER NOT NULL,
  how_won_agreed INTEGER NOT NULL,   -- rounds where Jev's how_won matched code's
  headlines_dropped INTEGER NOT NULL, -- headline picks code's checks rejected
  created_at INTEGER NOT NULL,
  PRIMARY KEY (match_id, parse_id)
);
