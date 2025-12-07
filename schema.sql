-- PostgreSQL schema for lb-scrape

CREATE TABLE IF NOT EXISTS scraper_targets (
    id           SERIAL PRIMARY KEY,
    name         VARCHAR(50) NOT NULL UNIQUE,
    url          VARCHAR(255) NOT NULL,
    healthy      BOOLEAN DEFAULT TRUE,
    last_checked TIMESTAMP NULL
);

CREATE TABLE IF NOT EXISTS scraper_jobs (
    id            BIGSERIAL PRIMARY KEY,
    job_type      VARCHAR(50) NOT NULL,
    payload       JSONB,
    status        VARCHAR(20) DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    target_id     INT REFERENCES scraper_targets(id),
    created_at    TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    started_at    TIMESTAMP NULL,
    completed_at  TIMESTAMP NULL,
    result        JSONB NULL,
    error_message TEXT NULL,
    retry_count   INT DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON scraper_jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_target_status ON scraper_jobs(target_id, status);

-- Sample data
INSERT INTO scraper_targets (name, url) VALUES
    ('vps-1', 'http://vps1.example.com:8080'),
    ('vps-2', 'http://vps2.example.com:8080')
ON CONFLICT (name) DO NOTHING;
