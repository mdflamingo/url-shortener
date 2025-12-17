ALTER TABLE urls ADD COLUMN user_id INTEGER;
ALTER TABLE urls ALTER COLUMN user_id SET NOT NULL;

CREATE INDEX idx_urls_user ON urls(users_id);
