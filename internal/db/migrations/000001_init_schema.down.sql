-- +migrate Down
-- +migrate Statement begin
DROP INDEX IF EXISTS idx_sources_provider;
DROP INDEX IF EXISTS idx_entries_source_url;
DROP INDEX IF EXISTS idx_entries_source;
DROP INDEX IF EXISTS idx_entries_ip;
DROP INDEX IF EXISTS idx_entries_host;
DROP INDEX IF EXISTS idx_entries_domain;
-- +migrate Statement end

-- +migrate Down
-- +migrate Statement begin
DROP TABLE IF EXISTS provider_processes;
-- +migrate Statement end

-- +migrate Down
-- +migrate Statement begin
DROP TABLE IF EXISTS entries;
-- +migrate Statement end

-- +migrate Down
-- +migrate Statement begin
DROP TABLE IF EXISTS sources;
-- +migrate Statement end

-- +migrate Down
-- +migrate Statement begin
DROP TABLE IF EXISTS providers;
-- +migrate Statement end