-- Runtime config snapshot: raw config file as read from the daemon.
-- Used so the UI can display previously-fetched configs without
-- re-requesting from the daemon on every page load.

-- name: InsertRuntimeConfigSnapshot :one
INSERT INTO runtime_config_snapshot (
    runtime_id, config_type, provider, raw_content, tool_version,
    content_hash, success, error_message
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListRuntimeConfigSnapshotsByRuntime :many
SELECT * FROM runtime_config_snapshot
WHERE runtime_id = $1
ORDER BY config_type, captured_at DESC;

-- name: GetLatestRuntimeConfigSnapshotsByRuntime :many
SELECT DISTINCT ON (config_type) *
FROM runtime_config_snapshot
WHERE runtime_id = $1
ORDER BY config_type, captured_at DESC;

-- Parsed / unified config: the LLM-structured, tool-agnostic representation.
-- Used for comparison and migration across providers / machines.

-- name: UpsertRuntimeConfigParsed :one
INSERT INTO runtime_config_parsed (
    runtime_id, config_type, unified_schema, snapshot_id, parsed_by,
    schema_version, unknown_keys, warnings
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (runtime_id, config_type)
DO UPDATE SET
    unified_schema = EXCLUDED.unified_schema,
    snapshot_id = EXCLUDED.snapshot_id,
    parsed_by = EXCLUDED.parsed_by,
    schema_version = EXCLUDED.schema_version,
    unknown_keys = EXCLUDED.unknown_keys,
    warnings = EXCLUDED.warnings,
    updated_at = now()
RETURNING *;

-- name: GetRuntimeConfigParsed :one
SELECT * FROM runtime_config_parsed
WHERE runtime_id = $1 AND config_type = $2;

-- name: ListRuntimeConfigParsedByRuntime :many
SELECT * FROM runtime_config_parsed
WHERE runtime_id = $1
ORDER BY config_type;
