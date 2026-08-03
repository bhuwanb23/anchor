-- Migration 011: Add backup verification columns and verification_configs table
-- Adds verification_status and verification_error to backup_jobs
-- Creates backup_verification_configs for per-server verification scheduling

-- Add verification columns to backup_jobs
ALTER TABLE backup_jobs ADD COLUMN verification_status TEXT DEFAULT '';
ALTER TABLE backup_jobs ADD COLUMN verification_error TEXT DEFAULT '';

-- Create verification configs table for per-server verification scheduling
CREATE TABLE IF NOT EXISTS backup_verification_configs (
    id TEXT PRIMARY KEY,
    server_id TEXT NOT NULL UNIQUE,
    last_verification_at TEXT,
    next_verification_at TEXT,
    last_full_verification_at TEXT,
    next_full_verification_at TEXT,
    verify_interval_hours INTEGER DEFAULT 168,    -- 7 days (weekly)
    full_verify_interval_hours INTEGER DEFAULT 720, -- 30 days (monthly)
    created_at TEXT DEFAULT (datetime('now')),
    updated_at TEXT DEFAULT (datetime('now')),
    FOREIGN KEY (server_id) REFERENCES servers(id)
);

-- Create index for efficient server lookups
CREATE INDEX IF NOT EXISTS idx_backup_verification_configs_server
    ON backup_verification_configs(server_id);
