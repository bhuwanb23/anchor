ALTER TABLE servers ADD COLUMN agent_id TEXT;
ALTER TABLE servers ADD COLUMN agent_secret_hash TEXT;
ALTER TABLE servers ADD COLUMN os_info TEXT;
ALTER TABLE servers ADD COLUMN arch TEXT;
ALTER TABLE servers ADD COLUMN ram_mb INTEGER;
ALTER TABLE servers ADD COLUMN disk_gb INTEGER;
ALTER TABLE servers ADD COLUMN ip_address TEXT;
CREATE INDEX IF NOT EXISTS idx_servers_agent_id ON servers(agent_id);
