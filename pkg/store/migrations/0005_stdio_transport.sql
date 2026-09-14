-- Migration 0005: stdio transport for local MCP servers (Phase 11b).

ALTER TABLE mcp_servers ADD COLUMN transport TEXT NOT NULL DEFAULT 'http';
ALTER TABLE mcp_servers ADD COLUMN command TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_servers ADD COLUMN args TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_servers ADD COLUMN env TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_servers ADD COLUMN workdir TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_servers ADD COLUMN cwd_isolation INTEGER NOT NULL DEFAULT 1;
