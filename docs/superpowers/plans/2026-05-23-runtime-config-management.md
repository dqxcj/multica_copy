# Runtime Configuration Management — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-runtime config read/write/compare/migrate support for 7 config types (skills, MCP, hooks, permissions, memory, rules, instructions) across Claude Code, Codex, OpenCode, and Hermes.

**Architecture:** Pull-based: server queues a config read/write request in a store, daemon picks it up on heartbeat, reads/writes provider config files with backup, reports result. LLM converts raw → unified JSON schema ← native format for comparison and migration.

**Tech Stack:** Go (chi router, pgx, sqlc), TypeScript (React, TanStack Query, Tailwind CSS), PostgreSQL, Anthropic API (LLM parsing)

---

### Task 1: DB Migration

**Files:**
- Already created: `server/migrations/109_runtime_config_store.up.sql`
- Already created: `server/migrations/109_runtime_config_store.down.sql`
- Create: `server/pkg/db/queries/runtime_config.sql`

- [ ] **Step 1: Write SQL queries for runtime_config_snapshot and runtime_config_parsed**

```sql
-- name: InsertRuntimeConfigSnapshot :one
INSERT INTO runtime_config_snapshot (
    runtime_id, config_type, provider, raw_content, tool_version,
    content_hash, success, error_message
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetLatestRuntimeConfigSnapshot :one
SELECT * FROM runtime_config_snapshot
WHERE runtime_id = $1 AND config_type = $2
ORDER BY captured_at DESC LIMIT 1;

-- name: ListRuntimeConfigSnapshotsByRuntime :many
SELECT * FROM runtime_config_snapshot
WHERE runtime_id = $1
ORDER BY config_type, captured_at DESC;

-- name: GetLatestRuntimeConfigSnapshotsByRuntime :many
SELECT DISTINCT ON (config_type) *
FROM runtime_config_snapshot
WHERE runtime_id = $1
ORDER BY config_type, captured_at DESC;

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

-- name: DeleteRuntimeConfigParsed :exec
DELETE FROM runtime_config_parsed WHERE runtime_id = $1 AND config_type = $2;
```

- [ ] **Step 2: Run sqlc to regenerate models and queries**

```bash
cd D:/Code/multica/server && sqlc generate
```

Expected: New methods on `*db.Queries` for the config tables.

- [ ] **Step 3: Commit**

```bash
git add server/migrations/109_runtime_config_store.*.sql server/pkg/db/queries/runtime_config.sql server/pkg/db/generated/
git commit -m "feat: add runtime_config_snapshot and runtime_config_parsed tables"
```

---

### Task 2: Protocol Types for Config Read/Write

**Files:**
- Modify: `server/pkg/protocol/messages.go:133-146` (extend heartbeat ack)
- Modify: `server/pkg/protocol/messages.go:177` (add pending types after PendingLocalSkillImport)

- [ ] **Step 1: Add config pending types and extend heartbeat ack**

After `DaemonHeartbeatPendingLocalSkillImport` (line 176), add:

```go
// DaemonHeartbeatPendingConfigRead requests the daemon to read all config
// types for a specific provider and report the raw file contents.
type DaemonHeartbeatPendingConfigRead struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
}

// DaemonHeartbeatPendingConfigWrite requests the daemon to write modified
// config files back to disk for a specific provider.
type DaemonHeartbeatPendingConfigWrite struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	// Configs contains the full ProviderConfigs to write (serialised as JSON).
	Configs json.RawMessage `json:"configs"`
}
```

In `DaemonHeartbeatAckPayload` (line 133), add after `PendingLocalSkillImports`:

```go
PendingConfigRead  *DaemonHeartbeatPendingConfigRead  `json:"pending_config_read,omitempty"`
PendingConfigWrite *DaemonHeartbeatPendingConfigWrite `json:"pending_config_write,omitempty"`
```

- [ ] **Step 2: Run `go build` to verify no compilation errors**

```bash
cd D:/Code/multica/server && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add server/pkg/protocol/messages.go
git commit -m "feat: add pending_config_read/write to heartbeat protocol"
```

---

### Task 3: Daemon Config Types and Reader

**Files:**
- Create: `server/internal/daemon/runtime_config_types.go`
- Create: `server/internal/daemon/runtime_config_reader.go`

- [ ] **Step 1: Define daemon config types**

```go
// server/internal/daemon/runtime_config_types.go
package daemon

const maxConfigFileSize = 1 << 20   // 1MB per file
const maxConfigFilesPerDir = 100    // cap directory listing

// ConfigFile represents a single config file.
type ConfigFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	FileType string `json:"file_type"` // "json", "toml", "yaml", "markdown"
}

// ProviderConfigs holds all config files for one provider.
type ProviderConfigs struct {
	Provider     string       `json:"provider"`
	Version      string       `json:"version"`
	Supported    bool         `json:"supported"`
	Skills       []ConfigFile `json:"skills,omitempty"`
	MCP          *ConfigFile  `json:"mcp,omitempty"`
	Hooks        *ConfigFile  `json:"hooks,omitempty"`
	Permissions  *ConfigFile  `json:"permissions,omitempty"`
	Memory       []ConfigFile `json:"memory,omitempty"`
	Rules        []ConfigFile `json:"rules,omitempty"`
	Instructions []ConfigFile `json:"instructions,omitempty"`
}
```

- [ ] **Step 2: Write ConfigReader dispatcher**

```go
// server/internal/daemon/runtime_config_reader.go
package daemon

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ReadProviderConfigs discovers and reads all config files for a provider.
func ReadProviderConfigs(provider string) (*ProviderConfigs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}

	switch provider {
	case "claude":
		return readClaudeConfigs(home)
	case "codex":
		return readCodexConfigs(home)
	case "opencode":
		return readOpencodeConfigs(home)
	case "hermes":
		return readHermesConfigs(home)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}
```

- [ ] **Step 3: Write Claude ConfigReader**

```go
func readClaudeConfigs(home string) (*ProviderConfigs, error) {
	base := filepath.Join(home, ".claude")
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return &ProviderConfigs{Provider: "claude", Supported: false}, nil
	}

	configs := &ProviderConfigs{Provider: "claude", Supported: true}
	configs.Version, _ = getClaudeVersion()

	// skills — reuse existing pattern from localSkillRootForProvider
	skillRoot := filepath.Join(home, ".claude", "skills")
	configs.Skills = readSkillFiles(skillRoot, "markdown")

	// MCP — ~/.claude.json or ~/.claude/.mcp.json
	if data, err := os.ReadFile(filepath.Join(home, ".claude", ".mcp.json")); err == nil {
		configs.MCP = &ConfigFile{Path: "~/.claude/.mcp.json", Content: string(data), FileType: "json"}
	} else if data, err := os.ReadFile(filepath.Join(home, ".claude.json")); err == nil {
		configs.MCP = &ConfigFile{Path: "~/.claude.json", Content: string(data), FileType: "json"}
	}

	// hooks & permissions — both in settings.json
	if data, err := os.ReadFile(filepath.Join(base, "settings.json")); err == nil {
		configs.Hooks = &ConfigFile{Path: "~/.claude/settings.json", Content: string(data), FileType: "json"}
		configs.Permissions = &ConfigFile{Path: "~/.claude/settings.json", Content: string(data), FileType: "json"}
	}

	// rules
	configs.Rules = readMarkdownDir(filepath.Join(base, "rules"))

	// memory
	configs.Memory = readMemoryFiles(filepath.Join(home, ".claude", "projects"), ".md")

	// instructions — CLAUDE.md
	if data, err := os.ReadFile(filepath.Join(base, "CLAUDE.md")); err == nil {
		configs.Instructions = append(configs.Instructions,
			ConfigFile{Path: "~/.claude/CLAUDE.md", Content: string(data), FileType: "markdown"})
	}

	return configs, nil
}
```

- [ ] **Step 4: Write Codex ConfigReader**

```go
func readCodexConfigs(home string) (*ProviderConfigs, error) {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	if _, err := os.Stat(codexHome); os.IsNotExist(err) {
		return &ProviderConfigs{Provider: "codex", Supported: false}, nil
	}

	configs := &ProviderConfigs{Provider: "codex", Supported: true}
	configs.Version, _ = getCodexVersion()

	// skills
	configs.Skills = readSkillFiles(filepath.Join(codexHome, "skills"), "markdown")

	// MCP, hooks, permissions — all in config.toml
	if data, err := os.ReadFile(filepath.Join(codexHome, "config.toml")); err == nil {
		content := string(data)
		configs.MCP = &ConfigFile{Path: "~/.codex/config.toml", Content: content, FileType: "toml"}
		configs.Permissions = &ConfigFile{Path: "~/.codex/config.toml", Content: content, FileType: "toml"}
	}

	// hooks — hooks.json or inline in config.toml
	if data, err := os.ReadFile(filepath.Join(codexHome, "hooks.json")); err == nil {
		configs.Hooks = &ConfigFile{Path: "~/.codex/hooks.json", Content: string(data), FileType: "json"}
	}

	// rules
	configs.Rules = readMarkdownDir(filepath.Join(codexHome, "rules"))

	// memories
	if data, err := os.ReadFile(filepath.Join(codexHome, "memories")); err == nil {
		configs.Memory = readMemoryFiles(filepath.Join(codexHome, "memories"), ".md")
	}

	// instructions
	if data, err := os.ReadFile(filepath.Join(codexHome, "AGENTS.md")); err == nil {
		configs.Instructions = append(configs.Instructions,
			ConfigFile{Path: "~/.codex/AGENTS.md", Content: string(data), FileType: "markdown"})
	}
	if data, err := os.ReadFile(filepath.Join(codexHome, "instructions.md")); err == nil {
		configs.Instructions = append(configs.Instructions,
			ConfigFile{Path: "~/.codex/instructions.md", Content: string(data), FileType: "markdown"})
	}

	return configs, nil
}
```

- [ ] **Step 5: Write OpenCode ConfigReader**

```go
func readOpencodeConfigs(home string) (*ProviderConfigs, error) {
	base := filepath.Join(home, ".config", "opencode")
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return &ProviderConfigs{Provider: "opencode", Supported: false}, nil
	}

	configs := &ProviderConfigs{Provider: "opencode", Supported: true}
	configs.Version, _ = getOpencodeVersion()

	// skills
	configs.Skills = readSkillFiles(filepath.Join(base, "skills"), "markdown")

	// MCP, permissions — in opencode.json
	if data, err := os.ReadFile(filepath.Join(base, "opencode.json")); err == nil {
		content := string(data)
		configs.MCP = &ConfigFile{Path: "~/.config/opencode/opencode.json", Content: content, FileType: "json"}
		configs.Permissions = &ConfigFile{Path: "~/.config/opencode/opencode.json", Content: content, FileType: "json"}
	}

	// hooks — OpenCode uses plugin-driven hooks, no static files
	configs.Hooks = nil

	// rules
	configs.Rules = readMarkdownDir(filepath.Join(base, "rules"))

	// instructions — AGENTS.md (also reads CLAUDE.md as fallback)
	if data, err := os.ReadFile(filepath.Join(base, "AGENTS.md")); err == nil {
		configs.Instructions = append(configs.Instructions,
			ConfigFile{Path: "~/.config/opencode/AGENTS.md", Content: string(data), FileType: "markdown"})
	}

	return configs, nil
}
```

- [ ] **Step 6: Write Hermes ConfigReader**

```go
func readHermesConfigs(home string) (*ProviderConfigs, error) {
	base := filepath.Join(home, ".hermes")
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return &ProviderConfigs{Provider: "hermes", Supported: false}, nil
	}

	configs := &ProviderConfigs{Provider: "hermes", Supported: true}
	configs.Version, _ = getHermesVersion()

	// skills
	configs.Skills = readSkillFiles(filepath.Join(base, "skills"), "markdown")

	// MCP, permissions — in config.yaml
	if data, err := os.ReadFile(filepath.Join(base, "config.yaml")); err == nil {
		content := string(data)
		configs.MCP = &ConfigFile{Path: "~/.hermes/config.yaml", Content: content, FileType: "yaml"}
		configs.Permissions = &ConfigFile{Path: "~/.hermes/config.yaml", Content: content, FileType: "yaml"}
	}

	// hooks — ~/.hermes/hooks/ directory
	if entries, err := os.ReadDir(filepath.Join(base, "hooks")); err == nil {
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") || len(configs.Hooks) >= maxConfigFilesPerDir {
				continue
			}
			p := filepath.Join(base, "hooks", e.Name())
			if data, err := os.ReadFile(p); err == nil && int64(len(data)) <= maxConfigFileSize {
				configs.Hooks = append(configs.Hooks, ConfigFile{
					Path: "~/.hermes/hooks/" + e.Name(), Content: string(data), FileType: "text",
				})
			}
		}
	}

	// memories — MEMORY.md + USER.md
	if data, err := os.ReadFile(filepath.Join(base, "memories", "MEMORY.md")); err == nil {
		configs.Memory = append(configs.Memory,
			ConfigFile{Path: "~/.hermes/memories/MEMORY.md", Content: string(data), FileType: "markdown"})
	}
	if data, err := os.ReadFile(filepath.Join(base, "memories", "USER.md")); err == nil {
		configs.Memory = append(configs.Memory,
			ConfigFile{Path: "~/.hermes/memories/USER.md", Content: string(data), FileType: "markdown"})
	}

	// instructions — SOUL.md + AGENTS.md
	if data, err := os.ReadFile(filepath.Join(base, "SOUL.md")); err == nil {
		configs.Instructions = append(configs.Instructions,
			ConfigFile{Path: "~/.hermes/SOUL.md", Content: string(data), FileType: "markdown"})
	}

	return configs, nil
}
```

- [ ] **Step 7: Write helper functions**

```go
func readSkillFiles(root string, fileType string) []ConfigFile {
	var files []ConfigFile
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if err != nil {
				return nil
			}
			// enforce depth limit
			rel, _ := filepath.Rel(root, path)
			if strings.Count(rel, string(filepath.Separator)) > maxLocalSkillDirDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || len(files) >= maxConfigFilesPerDir {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || int64(len(data)) > maxConfigFileSize {
			return nil
		}
		files = append(files, ConfigFile{
			Path: strings.Replace(path, root, "~"+filepath.Base(root), 1),
			Content:  string(data),
			FileType: fileType,
		})
		return nil
	})
	return files
}

func readMarkdownDir(dir string) []ConfigFile {
	var files []ConfigFile
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || len(files) >= maxConfigFilesPerDir {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil || int64(len(data)) > maxConfigFileSize {
			continue
		}
		files = append(files, ConfigFile{
			Path:     p,
			Content:  string(data),
			FileType: "markdown",
		})
	}
	return files
}

func readMemoryFiles(root string, ext string) []ConfigFile {
	var files []ConfigFile
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(files) >= maxConfigFilesPerDir {
			return nil
		}
		if !strings.Contains(path, "memory") {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ext) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || int64(len(data)) > maxConfigFileSize {
			return nil
		}
		files = append(files, ConfigFile{
			Path:     path,
			Content:  string(data),
			FileType: "markdown",
		})
		return nil
	})
	return files
}

// Version detection — attempt to get installed version for each tool
func getClaudeVersion() (string, bool) { return getVersionOutput("claude", "--version") }
func getCodexVersion() (string, bool) { return getVersionOutput("codex", "--version") }
func getOpencodeVersion() (string, bool) { return getVersionOutput("opencode", "--version") }
func getHermesVersion() (string, bool) { return getVersionOutput("hermes", "--version") }

func getVersionOutput(cmd string, arg string) (string, bool) {
	path, err := exec.LookPath(cmd)
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, arg).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}
```

- [ ] **Step 8: Run `go build` to verify**

```bash
cd D:/Code/multica/server && go build ./...
```

- [ ] **Step 9: Commit**

```bash
git add server/internal/daemon/runtime_config_types.go server/internal/daemon/runtime_config_reader.go
git commit -m "feat: add ConfigReader for claude/codex/opencode/hermes"
```

---

### Task 4: Daemon ConfigWriter with Backup

**Files:**
- Create: `server/internal/daemon/runtime_config_writer.go`

- [ ] **Step 1: Write ConfigWriter with backup mechanism**

```go
// server/internal/daemon/runtime_config_writer.go
package daemon

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var configWriteMu sync.Mutex

// WriteProviderConfigs writes config files back to disk with .bak backups.
// Returns paths of created backup files.
func WriteProviderConfigs(configs *ProviderConfigs) ([]string, error) {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}

	var backups []string

	writeCfg := func(origPath string, cfg *ConfigFile) error {
		if cfg == nil {
			return nil
		}
		// Resolve ~ to actual home
		realPath := resolvePath(origPath, home)
		bakPath, err := writeBackup(realPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("backup %s: %w", realPath, err)
		}
		if bakPath != "" {
			backups = append(backups, bakPath)
		}
		if err := os.WriteFile(realPath, []byte(cfg.Content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", realPath, err)
		}
		return nil
	}

	// Write structured configs
	switch configs.Provider {
	case "claude":
		if err := writeCfg(filepath.Join(home, ".claude", "settings.json"), configs.Hooks); err != nil {
			return backups, err
		}
		if err := writeCfg(filepath.Join(home, ".claude", ".mcp.json"), configs.MCP); err != nil {
			return backups, err
		}
	case "codex":
		codexHome := codexDir(home)
		if err := writeCfg(filepath.Join(codexHome, "config.toml"), configs.MCP); err != nil {
			return backups, err
		}
	case "opencode":
		base := filepath.Join(home, ".config", "opencode")
		if err := writeCfg(filepath.Join(base, "opencode.json"), configs.MCP); err != nil {
			return backups, err
		}
	case "hermes":
		base := filepath.Join(home, ".hermes")
		if err := writeCfg(filepath.Join(base, "config.yaml"), configs.MCP); err != nil {
			return backups, err
		}
	}

	return backups, nil
}

func writeBackup(src string) (string, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	bakPath := src + ".bak." + time.Now().Format("20060102-150405")
	if err := os.WriteFile(bakPath, data, 0644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return bakPath, nil
}

func resolvePath(path string, home string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		return filepath.Join(home, path[1:])
	}
	return path
}

func codexDir(home string) string {
	if d := strings.TrimSpace(os.Getenv("CODEX_HOME")); d != "" {
		return d
	}
	return filepath.Join(home, ".codex")
}

// RestoreBackup copies a .bak file back to the original path.
func RestoreBackup(bakPath string) error {
	if !strings.Contains(bakPath, ".bak.") {
		return fmt.Errorf("not a backup file: %s", bakPath)
	}
	origPath := strings.SplitN(bakPath, ".bak.", 2)[0]
	data, err := os.ReadFile(bakPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	return os.WriteFile(origPath, data, 0644)
}
```

- [ ] **Step 2: Run `go build`**

```bash
cd D:/Code/multica/server && go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add server/internal/daemon/runtime_config_writer.go
git commit -m "feat: add ConfigWriter with .bak backup mechanism"
```

---

### Task 5: Config Read/Write Store Interface + In-Memory Implementation

**Files:**
- Create: `server/internal/handler/runtime_config_store.go`

- [ ] **Step 1: Define store interfaces and types**

```go
// server/internal/handler/runtime_config_store.go
package handler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
)

type ConfigRequestStatus string

const (
	ConfigRequestPending   ConfigRequestStatus = "pending"
	ConfigRequestRunning   ConfigRequestStatus = "running"
	ConfigRequestCompleted ConfigRequestStatus = "completed"
	ConfigRequestFailed    ConfigRequestStatus = "failed"
	ConfigRequestTimeout   ConfigRequestStatus = "timeout"
)

type RuntimeConfigReadRequest struct {
	ID        string                  `json:"id"`
	RuntimeID string                  `json:"runtime_id"`
	Provider  string                  `json:"provider"`
	Status    ConfigRequestStatus     `json:"status"`
	Configs   *daemon.ProviderConfigs `json:"configs,omitempty"`
	Error     string                  `json:"error,omitempty"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

type RuntimeConfigWriteRequest struct {
	ID        string                `json:"id"`
	RuntimeID string                `json:"runtime_id"`
	Provider  string                `json:"provider"`
	Configs   *daemon.ProviderConfigs `json:"configs"`
	Status    ConfigRequestStatus   `json:"status"`
	Backups   []string              `json:"backups,omitempty"`
	Error     string                `json:"error,omitempty"`
	CreatedAt time.Time             `json:"created_at"`
	UpdatedAt time.Time             `json:"updated_at"`
}

// ConfigReadStore is the pending-config-read queue (mirrors LocalSkillListStore pattern).
type ConfigReadStore interface {
	Create(ctx context.Context, runtimeID, provider string) (*RuntimeConfigReadRequest, error)
	Get(ctx context.Context, id string) (*RuntimeConfigReadRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*RuntimeConfigReadRequest, error)
	Complete(ctx context.Context, id string, configs *daemon.ProviderConfigs) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// ConfigWriteStore is the pending-config-write queue.
type ConfigWriteStore interface {
	Create(ctx context.Context, runtimeID, provider string, configs *daemon.ProviderConfigs) (*RuntimeConfigWriteRequest, error)
	Get(ctx context.Context, id string) (*RuntimeConfigWriteRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*RuntimeConfigWriteRequest, error)
	Complete(ctx context.Context, id string, backups []string) error
	Fail(ctx context.Context, id string, errMsg string) error
}

// --- In-memory implementations (single-node, good enough for self-hosted) ---

type InMemoryConfigReadStore struct {
	mu       sync.Mutex
	requests map[string]*RuntimeConfigReadRequest
}

func NewInMemoryConfigReadStore() *InMemoryConfigReadStore {
	return &InMemoryConfigReadStore{requests: make(map[string]*RuntimeConfigReadRequest)}
}

func (s *InMemoryConfigReadStore) Create(_ context.Context, runtimeID, provider string) (*RuntimeConfigReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictRunning(runtimeID)
	now := time.Now()
	req := &RuntimeConfigReadRequest{
		ID:        randomID(),
		RuntimeID: runtimeID,
		Provider:  provider,
		Status:    ConfigRequestPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryConfigReadStore) evictRunning(runtimeID string) {
	for id, r := range s.requests {
		if r.RuntimeID == runtimeID && r.Status == ConfigRequestRunning {
			r.Status = ConfigRequestTimeout
			slog.Warn("config read request timed out", "request_id", id, "runtime_id", runtimeID)
		}
	}
}

func (s *InMemoryConfigReadStore) Get(_ context.Context, id string) (*RuntimeConfigReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.requests[id]
	if !ok {
		return nil, nil
	}
	return req, nil
}

func (s *InMemoryConfigReadStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.requests {
		if r.RuntimeID == runtimeID && r.Status == ConfigRequestPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryConfigReadStore) PopPending(_ context.Context, runtimeID string) (*RuntimeConfigReadRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.requests {
		if r.RuntimeID == runtimeID && r.Status == ConfigRequestPending {
			r.Status = ConfigRequestRunning
			r.UpdatedAt = time.Now()
			return r, nil
		}
	}
	return nil, nil
}

func (s *InMemoryConfigReadStore) Complete(_ context.Context, id string, configs *daemon.ProviderConfigs) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.requests[id]
	if !ok {
		return nil
	}
	req.Configs = configs
	req.Status = ConfigRequestCompleted
	req.UpdatedAt = time.Now()
	return nil
}

func (s *InMemoryConfigReadStore) Fail(_ context.Context, id string, errMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	req, ok := s.requests[id]
	if !ok {
		return nil
	}
	req.Status = ConfigRequestFailed
	req.Error = errMsg
	req.UpdatedAt = time.Now()
	return nil
}

// Similar implementation for InMemoryConfigWriteStore (same pattern)
```

- [ ] **Step 2: Commit**

```bash
git add server/internal/handler/runtime_config_store.go
git commit -m "feat: add ConfigReadStore and ConfigWriteStore interfaces + in-memory impl"
```

---

### Task 6: Heartbeat Integration for Config Queues

**Files:**
- Modify: `server/internal/handler/daemon.go:835-960` (add config probe/claim blocks)
- Modify: `server/internal/handler/handler.go:83-155` (add store fields)

- [ ] **Step 1: Add store fields to Handler struct**

In `handler.go`, add after `LocalSkillImportStore`:

```go
ConfigReadStore       ConfigReadStore
ConfigWriteStore      ConfigWriteStore
```

In `handler.go` `New()`, add after line 141:

```go
ConfigReadStore:       NewInMemoryConfigReadStore(),
ConfigWriteStore:      NewInMemoryConfigWriteStore(),
```

- [ ] **Step 2: Add config read probe/claim block in processHeartbeat**

In `daemon.go`, after the local-skill-import block (around line 960), add:

```go
	// Probe then claim the config-read queue.
	probeConfigReadStart := time.Now()
	probeConfigReadCtx, cancelProbeConfigRead := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasConfigRead, probeConfigReadErr := h.ConfigReadStore.HasPending(probeConfigReadCtx, runtimeID)
	cancelProbeConfigRead()
	m.ConfigReadMs = time.Since(probeConfigReadStart).Milliseconds()
	switch {
	case probeConfigReadErr == nil && hasConfigRead:
		pendingConfigRead, popErr := h.ConfigReadStore.PopPending(ctx, runtimeID)
		if popErr != nil {
			slog.Warn("config read PopPending failed", "error", popErr, "runtime_id", runtimeID)
		} else if pendingConfigRead != nil {
			ack.PendingConfigRead = &protocol.DaemonHeartbeatPendingConfigRead{
				ID:       pendingConfigRead.ID,
				Provider: pendingConfigRead.Provider,
			}
		}
	case probeConfigReadErr != nil:
		if errors.Is(probeConfigReadErr, context.DeadlineExceeded) || errors.Is(probeConfigReadErr, context.Canceled) {
			slog.Warn("config read HasPending timed out", "runtime_id", runtimeID)
		} else {
			slog.Warn("config read HasPending failed", "error", probeConfigReadErr, "runtime_id", runtimeID)
		}
	}

	// Probe then claim the config-write queue.
	probeConfigWriteStart := time.Now()
	probeConfigWriteCtx, cancelProbeConfigWrite := context.WithTimeout(ctx, heartbeatHasPendingTimeout)
	hasConfigWrite, probeConfigWriteErr := h.ConfigWriteStore.HasPending(probeConfigWriteCtx, runtimeID)
	cancelProbeConfigWrite()
	switch {
	case probeConfigWriteErr == nil && hasConfigWrite:
		pendingConfigWrite, popErr := h.ConfigWriteStore.PopPending(ctx, runtimeID)
		if popErr != nil {
			slog.Warn("config write PopPending failed", "error", popErr, "runtime_id", runtimeID)
		} else if pendingConfigWrite != nil {
			raw, _ := json.Marshal(pendingConfigWrite.Configs)
			ack.PendingConfigWrite = &protocol.DaemonHeartbeatPendingConfigWrite{
				ID:       pendingConfigWrite.ID,
				Provider: pendingConfigWrite.Provider,
				Configs:  raw,
			}
		}
	case probeConfigWriteErr != nil:
		if errors.Is(probeConfigWriteErr, context.DeadlineExceeded) || errors.Is(probeConfigWriteErr, context.Canceled) {
			slog.Warn("config write HasPending timed out", "runtime_id", runtimeID)
		} else {
			slog.Warn("config write HasPending failed", "error", probeConfigWriteErr, "runtime_id", runtimeID)
		}
	}
```

Add `ConfigReadMs` and `ConfigWriteMs` fields to the `heartbeatMetrics` struct.

- [ ] **Step 3: Run `go build`**

```bash
cd D:/Code/multica/server && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add server/internal/handler/daemon.go server/internal/handler/handler.go
git commit -m "feat: integrate config read/write queues into heartbeat"
```

---

### Task 7: Daemon Config Processing + Client Methods

**Files:**
- Modify: `server/internal/daemon/client.go`
- Modify: `server/internal/daemon/daemon.go`

- [ ] **Step 1: Add client methods for config read/write reporting**

In `client.go`, add:

```go
func (c *Client) ReportConfigReadResult(ctx context.Context, runtimeID, requestID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/config-read/%s/result", runtimeID, requestID), result, nil)
}

func (c *Client) ReportConfigWriteResult(ctx context.Context, runtimeID, requestID string, result map[string]any) error {
	return c.postJSON(ctx, fmt.Sprintf("/api/daemon/runtimes/%s/config-write/%s/result", runtimeID, requestID), result, nil)
}
```

- [ ] **Step 2: Add daemon handler for pending config read**

In `daemon.go`, add after the local-skill handling:

```go
func (d *Daemon) handleConfigRead(ctx context.Context, runtimeID string, pending *protocol.DaemonHeartbeatPendingConfigRead) {
	configs, err := ReadProviderConfigs(pending.Provider)
	result := map[string]any{}
	if err != nil {
		result["error"] = err.Error()
	} else {
		result["configs"] = configs
		result["supported"] = configs.Supported
	}
	if reportErr := d.client.ReportConfigReadResult(ctx, runtimeID, pending.ID, result); reportErr != nil {
		slog.Error("report config read failed", "error", reportErr, "request_id", pending.ID)
	}
}

func (d *Daemon) handleConfigWrite(ctx context.Context, runtimeID string, pending *protocol.DaemonHeartbeatPendingConfigWrite) {
	var configs ProviderConfigs
	if err := json.Unmarshal(pending.Configs, &configs); err != nil {
		d.client.ReportConfigWriteResult(ctx, runtimeID, pending.ID, map[string]any{"error": err.Error()})
		return
	}
	backups, err := WriteProviderConfigs(&configs)
	result := map[string]any{"backups": backups}
	if err != nil {
		result["error"] = err.Error()
	}
	d.client.ReportConfigWriteResult(ctx, runtimeID, pending.ID, result)
}
```

- [ ] **Step 3: Wire into heartbeat response processing loop**

In `daemon.go`, in the method that dispatches heartbeat ack pending actions, add:

```go
if resp.PendingConfigRead != nil {
	d.handleConfigRead(ctx, runtimeID, resp.PendingConfigRead)
}
if resp.PendingConfigWrite != nil {
	d.handleConfigWrite(ctx, runtimeID, resp.PendingConfigWrite)
}
```

- [ ] **Step 4: Commit**

```bash
git add server/internal/daemon/client.go server/internal/daemon/daemon.go
git commit -m "feat: handle config read/write in daemon heartbeat processing"
```

---

### Task 8: Server REST API — Config Read/Write/Query Endpoints

**Files:**
- Create: `server/internal/handler/runtime_config_api.go`

- [ ] **Step 1: Write user-facing config API handlers**

```go
// server/internal/handler/runtime_config_api.go
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// POST /api/runtimes/{runtimeId}/config/read
func (h *Handler) InitiateConfigRead(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		writeError(w, http.StatusBadRequest, "provider query parameter is required")
		return
	}
	req, err := h.ConfigReadStore.Create(r.Context(), runtimeID, provider)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

// GET /api/runtimes/{runtimeId}/config/read/{requestId}
func (h *Handler) GetConfigReadResult(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "requestId")
	req, err := h.ConfigReadStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req == nil {
		writeError(w, http.StatusNotFound, "config read request not found")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// GET /api/runtimes/{runtimeId}/config
func (h *Handler) GetRuntimeConfigs(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	// Return stored parsed configs from DB (no daemon interaction)
	parsed, err := h.Queries.ListRuntimeConfigParsedByRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, parsed)
}

// PUT /api/runtimes/{runtimeId}/config
func (h *Handler) InitiateConfigWrite(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	var body struct {
		Provider string                  `json:"provider"`
		Configs  *daemon.ProviderConfigs `json:"configs"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req, err := h.ConfigWriteStore.Create(r.Context(), runtimeID, body.Provider, body.Configs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

// GET /api/runtimes/{runtimeId}/config/diff
func (h *Handler) GetRuntimeConfigDiff(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	otherRuntimeID := r.URL.Query().Get("other_runtime_id")
	if otherRuntimeID == "" {
		writeError(w, http.StatusBadRequest, "other_runtime_id query parameter is required")
		return
	}

	// Fetch parsed configs for both runtimes
	left, _ := h.Queries.ListRuntimeConfigParsedByRuntime(r.Context(), parseUUID(runtimeID))
	right, _ := h.Queries.ListRuntimeConfigParsedByRuntime(r.Context(), parseUUID(otherRuntimeID))

	// Simple diff: compare unified_schema per config_type
	type DiffResult struct {
		ConfigType string          `json:"config_type"`
		Left       json.RawMessage `json:"left,omitempty"`
		Right      json.RawMessage `json:"right,omitempty"`
		Status     string          `json:"status"` // "same", "different", "left_only", "right_only"
	}

	diffs := computeConfigDiff(left, right)
	writeJSON(w, http.StatusOK, map[string]any{
		"source_runtime_id": runtimeID,
		"other_runtime_id": otherRuntimeID,
		"diffs":             diffs,
	})
}

func computeConfigDiff(left, right []db.RuntimeConfigParsed) []DiffResult {
	rightMap := make(map[string]db.RuntimeConfigParsed)
	for _, r := range right {
		rightMap[r.ConfigType] = r
	}
	var diffs []DiffResult
	seenRight := make(map[string]bool)
	for _, l := range left {
		r, ok := rightMap[l.ConfigType]
		if !ok {
			diffs = append(diffs, DiffResult{ConfigType: l.ConfigType, Left: l.UnifiedSchema, Status: "left_only"})
			continue
		}
		seenRight[l.ConfigType] = true
		if string(l.UnifiedSchema) == string(r.UnifiedSchema) {
			diffs = append(diffs, DiffResult{ConfigType: l.ConfigType, Status: "same"})
		} else {
			diffs = append(diffs, DiffResult{ConfigType: l.ConfigType, Left: l.UnifiedSchema, Right: r.UnifiedSchema, Status: "different"})
		}
	}
	for _, r := range right {
		if !seenRight[r.ConfigType] {
			diffs = append(diffs, DiffResult{ConfigType: r.ConfigType, Right: r.UnifiedSchema, Status: "right_only"})
		}
	}
	return diffs
}
```

- [ ] **Step 2: Write daemon-facing report endpoints**

```go
// POST /api/daemon/runtimes/{runtimeId}/config-read/{requestId}/result
func (h *Handler) ReportConfigReadResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	requestID := chi.URLParam(r, "requestId")

	var body struct {
		Configs   *daemon.ProviderConfigs `json:"configs"`
		Supported bool                    `json:"supported"`
		Error     string                  `json:"error"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if body.Error != "" {
		h.ConfigReadStore.Fail(r.Context(), requestID, body.Error)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	// Store raw configs in DB + parse via LLM (async)
	h.ConfigReadStore.Complete(r.Context(), requestID, body.Configs)
	go h.persistAndParseConfigs(context.Background(), runtimeID, body.Configs)

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// POST /api/daemon/runtimes/{runtimeId}/config-write/{requestId}/result
func (h *Handler) ReportConfigWriteResult(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "requestId")
	var body struct {
		Backups []string `json:"backups"`
		Error   string   `json:"error"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Error != "" {
		h.ConfigWriteStore.Fail(r.Context(), requestID, body.Error)
	} else {
		h.ConfigWriteStore.Complete(r.Context(), requestID, body.Backups)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// persistAndParseConfigs stores raw configs in DB, then calls LLM to parse into unified schema.
func (h *Handler) persistAndParseConfigs(ctx context.Context, runtimeID string, configs *daemon.ProviderConfigs) {
	if configs == nil {
		return
	}
	rtID := parseUUID(runtimeID)

	for _, ct := range AllConfigTypes {
		raw := configTypeRaw(configs, ct)
		if raw == nil {
			continue
		}
		hash := sha256Hex(raw.Content)
		snap, err := h.Queries.InsertRuntimeConfigSnapshot(ctx, db.InsertRuntimeConfigSnapshotParams{
			RuntimeID:    rtID,
			ConfigType:   string(ct),
			Provider:     configs.Provider,
			RawContent:   marshalRawContent(raw),
			ToolVersion:  configs.Version,
			ContentHash:  hash,
			Success:      true,
			ErrorMessage: "",
		})
		if err != nil {
			slog.Error("insert config snapshot failed", "error", err)
			continue
		}
		// Parse with LLM and upsert parsed entry
		if parsed := h.parseWithLLM(ctx, ct, configs.Provider, raw); parsed != nil {
			h.Queries.UpsertRuntimeConfigParsed(ctx, db.UpsertRuntimeConfigParsedParams{
				RuntimeID:     rtID,
				ConfigType:    string(ct),
				UnifiedSchema: parsed.Data,
				SnapshotID:    pgtype.UUID{Bytes: snap.ID, Valid: true},
				ParsedBy:      "anthropic",
				SchemaVersion: 1,
				UnknownKeys:   parsed.UnknownKeys,
				Warnings:      parsed.Warnings,
			})
		}
	}
}
```

- [ ] **Step 3: Commit**

```bash
git add server/internal/handler/runtime_config_api.go
git commit -m "feat: add config read/write/diff API endpoints"
```

---

### Task 9: Router Wiring

**Files:**
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Register daemon-facing endpoints**

In the daemon route group (after line ~283), add:

```go
r.Post("/runtimes/{runtimeId}/config-read/{requestId}/result", h.ReportConfigReadResult)
r.Post("/runtimes/{runtimeId}/config-write/{requestId}/result", h.ReportConfigWriteResult)
```

- [ ] **Step 2: Register user-facing endpoints**

In the `/api/runtimes/{runtimeId}` route group (after line ~591), add:

```go
r.Post("/config/read", h.InitiateConfigRead)
r.Get("/config/read/{requestId}", h.GetConfigReadResult)
r.Get("/config", h.GetRuntimeConfigs)
r.Put("/config", h.InitiateConfigWrite)
r.Get("/config/diff", h.GetRuntimeConfigDiff)
```

- [ ] **Step 3: Run go build**

```bash
cd D:/Code/multica/server && go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add server/cmd/server/router.go
git commit -m "feat: register config API routes"
```

---

### Task 10: LLM Integration — Raw ↔ Unified Schema Parsing

**Files:**
- Create: `server/internal/handler/runtime_config_llm.go`

- [ ] **Step 1: Define unified schemas and LLM parsing logic**

```go
package handler

// Config type constants and all config types list defined inline.
// LLM parsing uses the existing Anthropic client from the daemon/agent code.

// unifiedSchemas maps each config type to its JSON Schema for the LLM prompt.
var unifiedSchemas = map[ConfigType]string{
    ConfigTypeSkills: `{
  "type": "array",
  "items": {
    "type": "object",
    "properties": {
      "name": {"type": "string"},
      "description": {"type": "string"},
      "content": {"type": "string"},
      "files": {
        "type": "array",
        "items": {"path": {"type": "string"}, "content": {"type": "string"}}
      }
    },
    "required": ["name"]
  }
}`,
    ConfigTypeMCP: `{
  "type": "array",
  "items": {
    "type": "object",
    "properties": {
      "name": {"type": "string"},
      "type": {"enum": ["stdio", "sse", "http"]},
      "command": {"type": "string"},
      "args": {"type": "array", "items": {"type": "string"}},
      "url": {"type": "string"},
      "env": {"type": "object"},
      "enabled": {"type": "boolean"}
    },
    "required": ["name"]
  }
}`,
    // ... similar for Hooks, Permissions, Memory, Rules, Instructions
}

// parseWithLLM sends raw config to Anthropic for structured parsing.
// Returns nil if parsing fails (caller falls back to raw-only display).
func (h *Handler) parseWithLLM(ctx context.Context, configType ConfigType, provider string, raw *daemon.ConfigFile) *ConfigSchemaUnified {
    // Build prompt: "You are a config parser. Convert this {provider} {format}
    //  config of type {configType} into this JSON Schema: {schema}.
    //  Report unknown keys that don't match the schema."
    // Call Anthropic API with max_tokens=4096.
    // Return structured JSON + unknown_keys + warnings.
    return nil // TODO: integrate actual Anthropic call
}

// serializeWithLLM converts unified schema back to native format for writing.
func (h *Handler) serializeWithLLM(ctx context.Context, configType ConfigType, provider string, unified json.RawMessage) (*daemon.ConfigFile, error) {
    // Build prompt: "Convert this unified config of type {configType} into
    //  {provider}'s native format ({json/toml/yaml})."
    return nil, nil // TODO: integrate actual Anthropic call
}
```

Note: Phase 8 is a deferred implementation. The initial implementation stores raw configs; LLM parsing is invoked asynchronously but the Anthropic API integration itself will be completed when API access is available in the handler context.

- [ ] **Step 1: Mark Phase 8 as scaffolded for later completion**

```bash
git add server/internal/handler/runtime_config_llm.go
git commit -m "feat: scaffold LLM parsing stubs for config unification"
```

---

### Task 11: Frontend — API Client + Types + Query Hooks

**Files:**
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/types/agent.ts`
- Create: `packages/core/runtimes/runtime-config.ts`

- [ ] **Step 1: Add TypeScript types**

In `packages/core/types/agent.ts`, append:

```typescript
export type RuntimeConfigType = "skills" | "mcp" | "hooks" | "permissions" | "memory" | "rules" | "instructions";

export interface RuntimeConfigFile {
  path: string;
  content: string;
  file_type: string;
}

export interface ProviderConfigs {
  provider: string;
  version: string;
  supported: boolean;
  skills: RuntimeConfigFile[];
  mcp: RuntimeConfigFile | null;
  hooks: RuntimeConfigFile | null;
  permissions: RuntimeConfigFile | null;
  memory: RuntimeConfigFile[];
  rules: RuntimeConfigFile[];
  instructions: RuntimeConfigFile[];
}

export interface RuntimeConfigReadRequest {
  id: string;
  runtime_id: string;
  provider: string;
  status: "pending" | "running" | "completed" | "failed" | "timeout";
  configs?: ProviderConfigs;
  error?: string;
}

export interface RuntimeConfigParsed {
  id: string;
  runtime_id: string;
  config_type: RuntimeConfigType;
  unified_schema: Record<string, unknown>;
  snapshot_id: string | null;
  schema_version: number;
  unknown_keys: string[];
  warnings: string[];
}
```

- [ ] **Step 2: Add API client methods**

In `packages/core/api/client.ts`, add to the `ApiClient` class:

```typescript
async initiateConfigRead(runtimeId: string, provider: string): Promise<RuntimeConfigReadRequest> {
  return this.fetch(`/api/runtimes/${runtimeId}/config/read?provider=${encodeURIComponent(provider)}`, { method: "POST" });
}
async getConfigReadResult(runtimeId: string, requestId: string): Promise<RuntimeConfigReadRequest> {
  return this.fetch(`/api/runtimes/${runtimeId}/config/read/${requestId}`);
}
async getRuntimeConfigs(runtimeId: string): Promise<RuntimeConfigParsed[]> {
  return this.fetch(`/api/runtimes/${runtimeId}/config`);
}
async getRuntimeConfigDiff(runtimeId: string, otherRuntimeId: string): Promise<{ diffs: unknown[] }> {
  return this.fetch(`/api/runtimes/${runtimeId}/config/diff?other_runtime_id=${encodeURIComponent(otherRuntimeId)}`);
}
async initiateConfigWrite(runtimeId: string, provider: string, configs: ProviderConfigs): Promise<{ id: string }> {
  return this.fetch(`/api/runtimes/${runtimeId}/config`, { method: "PUT", body: JSON.stringify({ provider, configs }) });
}
```

- [ ] **Step 3: Create query hooks**

In `packages/core/runtimes/runtime-config.ts`:

```typescript
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { runtimeKeys } from "./queries";

export const runtimeConfigKeys = {
  all: (wsId: string) => [...runtimeKeys.all(wsId), "config"] as const,
  forRuntime: (wsId: string, rtId: string) => [...runtimeConfigKeys.all(wsId), rtId] as const,
  readRequest: (wsId: string, rtId: string, reqId: string) => [...runtimeConfigKeys.forRuntime(wsId, rtId), "read", reqId] as const,
};

export function useRuntimeConfigs(wsId: string, rtId: string) {
  return useQuery({
    queryKey: runtimeConfigKeys.forRuntime(wsId, rtId),
    queryFn: () => api.getRuntimeConfigs(rtId),
  });
}
```

- [ ] **Step 4: Commit**

```bash
git add packages/core/api/client.ts packages/core/types/agent.ts packages/core/runtimes/runtime-config.ts
git commit -m "feat: add frontend config API types, client methods, and query hooks"
```

---

### Task 12: Frontend — Config Management UI Components

**Files:**
- Create: `packages/views/runtimes/components/runtime-config-section.tsx`
- Create: `packages/views/runtimes/components/config-type-card.tsx`
- Create: `packages/views/runtimes/components/config-editor.tsx`
- Modify: `packages/views/runtimes/components/runtimes-page.tsx` (integrate)

- [ ] **Step 1: Write RuntimeConfigSection component**

```tsx
// packages/views/runtimes/components/runtime-config-section.tsx
"use client";

import { useState } from "react";
import { RefreshCw } from "lucide-react";
import { useRuntimeConfigs } from "@/core/runtimes/runtime-config";
import { ConfigTypeCard } from "./config-type-card";

interface Props {
  workspaceId: string;
  runtimeId: string;
  provider: string;
}

export function RuntimeConfigSection({ workspaceId, runtimeId, provider }: Props) {
  const { data: configs, isLoading, refetch } = useRuntimeConfigs(workspaceId, runtimeId);
  const [isRefreshing, setIsRefreshing] = useState(false);

  const handleRefresh = async () => {
    setIsRefreshing(true);
    // Trigger a config read from daemon, poll until complete
    const { id } = await api.initiateConfigRead(runtimeId, provider);
    // Poll pattern: retry every 2s until completed
    const poll = setInterval(async () => {
      const result = await api.getConfigReadResult(runtimeId, id);
      if (result.status === "completed" || result.status === "failed") {
        clearInterval(poll);
        setIsRefreshing(false);
        refetch();
      }
    }, 2000);
  };

  if (!configs || configs.length === 0) {
    return (
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold">Agent Configs</h3>
          <button onClick={handleRefresh} disabled={isRefreshing}
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
            <RefreshCw className={isRefreshing ? "animate-spin" : ""} size={14} />
            Refresh
          </button>
        </div>
        <p className="text-sm text-muted-foreground">No config data yet. Click Refresh to fetch from daemon.</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold">Agent Configs</h3>
        <button onClick={handleRefresh} disabled={isRefreshing}
          className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
          <RefreshCw className={isRefreshing ? "animate-spin" : ""} size={14} />
          Refresh
        </button>
      </div>
      <div className="space-y-2">
        {configs.map((c) => (
          <ConfigTypeCard key={c.config_type} config={c} runtimeId={runtimeId} workspaceId={workspaceId} />
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Write ConfigTypeCard component**

```tsx
// packages/views/runtimes/components/config-type-card.tsx
"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";

interface Props {
  config: RuntimeConfigParsed;
  runtimeId: string;
  workspaceId: string;
}

export function ConfigTypeCard({ config }: Props) {
  const [expanded, setExpanded] = useState(false);
  const configTypeLabel: Record<string, string> = {
    skills: "Skills", mcp: "MCP Config", hooks: "Hooks",
    permissions: "Permissions", memory: "Memory", rules: "Rules",
    instructions: "Instructions",
  };

  const itemCount = countItems(config);
  const warnings = config.unknown_keys?.length || 0;

  return (
    <div className="rounded-md border p-3">
      <button onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between text-left">
        <div className="flex items-center gap-2">
          {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
          <span className="text-sm font-medium">{configTypeLabel[config.config_type] || config.config_type}</span>
          <span className="text-xs text-muted-foreground">({itemCount} items)</span>
          {warnings > 0 && (
            <span className="rounded bg-amber-100 px-1.5 py-0.5 text-xs text-amber-700">
              {warnings} unknown keys
            </span>
          )}
        </div>
      </button>
      {expanded && (
        <div className="mt-2 max-h-96 overflow-auto">
          <pre className="whitespace-pre-wrap text-xs text-muted-foreground">
            {JSON.stringify(config.unified_schema, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}

function countItems(config: RuntimeConfigParsed): number {
  const schema = config.unified_schema as any;
  if (!schema) return 0;
  // Count based on config type
  if (Array.isArray(schema)) return schema.length;
  if (schema.skills) return schema.skills.length;
  if (schema.servers) return schema.servers.length;
  if (schema.hooks) return schema.hooks.length;
  if (schema.rules) return schema.rules.length;
  if (schema.files) return schema.files.length;
  return Object.keys(schema).length;
}
```

- [ ] **Step 3: Integrate into runtime detail page**

In `runtimes-page.tsx`, add `<RuntimeConfigSection>` below the existing UsageSection in the runtime detail panel.

- [ ] **Step 4: Run `pnpm build` to verify**

```bash
cd D:/Code/multica && pnpm --filter @multica/web build 2>&1 | tail -20
```

- [ ] **Step 5: Commit**

```bash
git add packages/views/runtimes/components/runtime-config-section.tsx packages/views/runtimes/components/config-type-card.tsx packages/views/runtimes/components/runtimes-page.tsx
git commit -m "feat: add runtime config section with config type cards"
```

---

### Task 13: Run E2E Tests with agent-browser

**Files:**
- None (test-only)

- [ ] **Step 1: Build and deploy updated server + web**

Build the Go server and Next.js frontend, deploy to Aliyun.

- [ ] **Step 2: Run E2E test with agent-browser**

```bash
# 1. Open Multica
agent-browser open "https://multica.dqxcj.top"
agent-browser snapshot -i
# 2. Navigate to runtimes page after login
agent-browser click @<login_button_ref>
agent-browser fill @<email_ref> "wf.ljy.sj@gmail.com"
agent-browser fill @<code_ref> "000000"
agent-browser click @<submit_ref>
agent-browser wait --url "/ai/runtimes"
# 3. Click on an online runtime to open detail
agent-browser snapshot -i
agent-browser click @<runtime_ref>
# 4. Find the Configs section
agent-browser snapshot -i
# 5. Click Refresh to fetch configs from daemon
agent-browser click @<refresh_button_ref>
agent-browser wait 5000
# 6. Verify config type cards render
agent-browser snapshot -i
# 7. Expand a card and verify content
agent-browser click @<card_expand_ref>
agent-browser snapshot -i
# Verify: Skills, MCP, Hooks, Permissions or "No config data yet" shown
agent-browser close
```

- [ ] **Step 3: Document test results in commit message or PR**

---

**Note**: `docs/superpowers/` 目录仅保留在本地，不提交到远程仓库。

---

### Task 14: Update Project Documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `AGENTS.md`
- Modify: `SELF_HOSTING.md`
- Modify: `SELF_HOSTING_ADVANCED.md`

- [ ] **Step 1: Add runtime config management section to CLAUDE.md / AGENTS.md**

Add a section documenting the new runtime config management architecture:

```markdown
## Runtime Configuration Management

Multica supports visual management and cross-machine migration of agent configurations
at the runtime level. Supported tools: Claude Code, Codex, OpenCode, Hermes.

Config types managed: Skills, MCP, Hooks, Permissions, Memory, Rules, Instructions.

### Architecture
- Daemon reads config files on-demand (not every heartbeat)
- Server stores raw snapshots + LLM-parsed unified schema
- LLM converts between native formats (JSON/TOML/YAML) and unified JSON Schema
- Writes create `<file>.bak.<timestamp>` backups before modification

### API Endpoints
- `POST /api/runtimes/{rt}/config/read` — Request daemon to read configs
- `GET /api/runtimes/{rt}/config` — Get stored parsed configs
- `PUT /api/runtimes/{rt}/config` — Queue a config write
- `GET /api/runtimes/{rt}/config/diff` — Compare two runtimes' configs

### Key Files
- `server/internal/daemon/runtime_config_reader.go` — Config file discovery
- `server/internal/daemon/runtime_config_writer.go` — Write with backup
- `server/internal/handler/runtime_config_api.go` — REST API handlers
- `server/internal/handler/runtime_config_store.go` — Pending request store
- `server/internal/handler/runtime_config_llm.go` — LLM parsing (stub)
- `packages/views/runtimes/components/runtime-config-section.tsx` — Frontend UI
```

- [ ] **Step 2: Add new tables to SELF_HOSTING.md schema notes**

Under the database section, add `runtime_config_snapshot` and `runtime_config_parsed` to the table listing.

- [ ] **Step 3: Add daemon config file discovery note to SELF_HOSTING_ADVANCED.md**

Document which directories the daemon reads for each provider (Claude/Codex/OpenCode/Hermes).

- [ ] **Step 4: Commit project docs only (NOT docs/superpowers/)**

```bash
git add CLAUDE.md AGENTS.md SELF_HOSTING.md SELF_HOSTING_ADVANCED.md
git commit -m "docs: add runtime config management architecture documentation"
```

---

## Summary

This plan covers 14 tasks that build the config management system end-to-end:

| Task | Layer | What |
|---|---|---|
| 1 | DB | Migration + queries |
| 2 | Protocol | Heartbeat pending types |
| 3 | Daemon | ConfigReader (4 providers) |
| 4 | Daemon | ConfigWriter + backup |
| 5 | Handler | Store interfaces + in-memory impl |
| 6 | Handler | Heartbeat integration |
| 7 | Daemon | Processing + client methods |
| 8 | Handler | REST API (6 user + 2 daemon endpoints) |
| 9 | Router | Route registration |
| 10 | Handler | LLM stubs |
| 11 | Frontend | API types, client, hooks |
| 12 | Frontend | Config section UI components |
| 13 | Test | E2E verification |
| 14 | Docs | Update project CLAUDE.md/AGENTS.md/SELF_HOSTING.md |
