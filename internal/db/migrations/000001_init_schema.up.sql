-- Initial schema: providers, sources, entries, provider_processes
CREATE TABLE IF NOT EXISTS providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT,
    trust_score REAL NOT NULL DEFAULT 0.5,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sources (
    id              TEXT PRIMARY KEY,
    provider_id     TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    source_url      TEXT NOT NULL,
    type            TEXT NOT NULL,
    trust_score     REAL,
    update_interval INTEGER,
    enabled         INTEGER NOT NULL DEFAULT 1,
    last_fetch_at   DATETIME,
    last_fetch_status TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS entries (
    id          TEXT PRIMARY KEY,
    process_id  TEXT,
    scheme      TEXT,
    domain      TEXT,
    host        TEXT,
    ip          TEXT,
    sub_domains TEXT,
    path        TEXT,
    raw_query   TEXT,
    source_url  TEXT,
    source      TEXT NOT NULL,
    category    TEXT,
    confidence  REAL DEFAULT 1.0,
    created_at  INTEGER,
    updated_at  INTEGER,
    deleted_at  INTEGER,
    UNIQUE (source_url, source, host)
);

CREATE TABLE IF NOT EXISTS provider_processes (
    id          TEXT PRIMARY KEY,
    status      TEXT,
    start_time  DATETIME,
    end_time    DATETIME,
    providers_processed TEXT,
    providers_removed   TEXT,
    error       TEXT
);

-- Indexes for entries table
CREATE INDEX IF NOT EXISTS idx_entries_domain ON entries(domain);
CREATE INDEX IF NOT EXISTS idx_entries_host ON entries(host);
CREATE INDEX IF NOT EXISTS idx_entries_ip ON entries(ip);
CREATE INDEX IF NOT EXISTS idx_entries_source ON entries(source);
CREATE INDEX IF NOT EXISTS idx_entries_source_url ON entries(source_url);

-- Indexes for sources
CREATE INDEX IF NOT EXISTS idx_sources_provider ON sources(provider_id);