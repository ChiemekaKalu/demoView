-- Opt-in gallery: a match is listed on the home page only while featured_at
-- is set (Unix ms, set by the upload-token holder). NULL means unlisted.
ALTER TABLE matches ADD COLUMN featured_at INTEGER;
CREATE INDEX matches_featured ON matches (featured_at) WHERE featured_at IS NOT NULL;
