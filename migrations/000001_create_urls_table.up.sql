CREATE TABLE IF NOT EXISTS urls (
    id SERIAL PRIMARY KEY,
    short_url VARCHAR(255) NOT NULL,
    full_url VARCHAR NOT NULL UNIQUE
);

CREATE INDEX idx_urls_short_urls ON urls(short_url);
