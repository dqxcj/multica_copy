-- Runtime config snapshots: raw files as read from the daemon for a
-- specific provider on a specific runtime. Stored so the UI can display
-- previously-fetched configs without re-requesting from the daemon on every
-- page load.
CREATE TABLE runtime_config_snapshot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    -- One of: skills, mcp, hooks, permissions, memory, rules, instructions
    config_type TEXT NOT NULL,
    -- claude, codex, opencode, hermes
    provider TEXT NOT NULL,
    -- Raw content as read from disk (JSON for structured files, text for
    -- markdown, etc.)
    raw_content JSONB NOT NULL DEFAULT '{}',
    -- Tool version string reported by the daemon at read time so the UI can
    -- flag schema drift after tool upgrades.
    tool_version TEXT NOT NULL DEFAULT '',
    -- SHA-256 over the raw content; used for change detection (skip re-parse
    -- if nothing changed).
    content_hash TEXT NOT NULL DEFAULT '',
    success BOOLEAN NOT NULL DEFAULT true,
    error_message TEXT NOT NULL DEFAULT '',
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_config_snapshot_runtime ON runtime_config_snapshot(runtime_id);
CREATE INDEX idx_config_snapshot_runtime_type ON runtime_config_snapshot(runtime_id, config_type);

-- Parsed / unified config: the LLM-structured, tool-agnostic representation.
-- Used for comparison and migration across providers / machines.
CREATE TABLE runtime_config_parsed (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    config_type TEXT NOT NULL,
    -- Tool-agnostic unified JSON (conforms to the JSON Schema for this type)
    unified_schema JSONB NOT NULL DEFAULT '{}',
    -- The snapshot this was parsed from (nullable — may be re-parsed later)
    snapshot_id UUID REFERENCES runtime_config_snapshot(id) ON DELETE SET NULL,
    -- LLM model used for parsing (for auditing)
    parsed_by TEXT NOT NULL DEFAULT '',
    schema_version INTEGER NOT NULL DEFAULT 1,
    -- Fields the LLM reported as unrecognised (upstream tool drift indicator)
    unknown_keys TEXT[],
    warnings TEXT[],
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (runtime_id, config_type)
);

CREATE INDEX idx_config_parsed_runtime ON runtime_config_parsed(runtime_id);
