-- Tables from the spec's "D1 tables" section, verbatim.

CREATE TABLE matches (
  id TEXT PRIMARY KEY,          -- random slug, used in the share link
  status TEXT NOT NULL,         -- uploaded | queued | parsing | ready | failed
  map TEXT, score_a INTEGER, score_b INTEGER,
  demo_key TEXT NOT NULL, demo_sha256 TEXT,
  parser_version TEXT, schema_version INTEGER,
  error TEXT, created_at INTEGER NOT NULL, parsed_at INTEGER
);
CREATE TABLE players (
  match_id TEXT, idx INTEGER, steam_id TEXT, name TEXT, team TEXT,
  kills INTEGER, deaths INTEGER, assists INTEGER, adr REAL, hs_pct REAL,
  PRIMARY KEY (match_id, idx)
);
CREATE TABLE rounds (
  match_id TEXT, n INTEGER, winner TEXT, reason TEXT, side_a TEXT,
  PRIMARY KEY (match_id, n)
);
CREATE TABLE kills (
  match_id TEXT, round INTEGER, tick INTEGER,
  attacker INTEGER, victim INTEGER, weapon TEXT, headshot INTEGER
);
