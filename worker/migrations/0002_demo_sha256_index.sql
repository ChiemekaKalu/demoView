-- A confirmed upload looks up its demo's hash to reuse a match that already
-- has the same demo.
CREATE INDEX matches_demo_sha256 ON matches (demo_sha256);
