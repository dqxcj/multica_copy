# Runtime Configuration Management — Design Spec

## Context

Multica 当前 daemon 只上报 skills（通过 `pending_local_skills` 心跳）。用户需要在运行时页面对 Claude Code / Codex / OpenCode / Hermes 的全部配置（Skills、MCP、Hooks、Permissions、Memory、Rules、Instructions）进行可视化管理，支持跨机器对比和迁移。

四台机器的实际配置分布已在 Phase 0 完成确认。

## Key Decisions

- **LLM provider**: 复用服务端已有的 Anthropic API client（`server/internal/handler/` 中已存在），不需要新引入外部依赖
- **未安装工具处理**: ConfigReader 发现工具未安装时返回 `supported: false`，前端显示 "未安装" 状态
- **统一 Schema 设计**: 七种配置类型各有 JSON Schema，在 Phase 8 中硬编码。见下方 Unified Schemas 节
- **写锁**: ConfigWriter 使用 `sync.Mutex`，进行中的写入会被新请求拒绝（返回 409）

## Unified Schemas (Phase 8 设计依据)

每种配置类型标准化为 JSON 结构，屏蔽四种工具的格式差异：

| Type | Unified Schema 关键字段 |
|---|---|
| Skills | `{ skills: [{ name, description, content, files: [{ path, content }] }] }` |
| MCP | `{ servers: [{ name, type: "stdio"\|"sse"\|"http", command?, args?, url?, env: {}, enabled: bool }] }` |
| Hooks | `{ hooks: [{ event: str, matcher: str, command: str, timeout: int }] }` |
| Permissions | `{ rules: [{ tool: str, pattern: str, action: "allow"\|"ask"\|"deny" }], approval_policy: str }` |
| Memory | `{ files: [{ path: str, content: str }] }` |
| Rules | `{ rules: [{ path: str, content: str, description: str, globs: [str] }] }` |
| Instructions | `{ files: [{ path: str, content: str, role: "system"\|"user" }] }` |

## Architecture

**按需拉取 + LLM 双层校验**

```
Daemon (read) → Server (raw snapshot) → LLM (parse → unified JSON schema) → Frontend
                                       ↓
                                  unknown_keys 检测（官方结构调整告警）
Frontend → unified schema → LLM (serialize → native format) → Server → Daemon (write + .bak)
```

- Daemon 仅在 server 请求时读取配置文件（非心跳持续上报）
- LLM 在写入/合并/迁移时做 schema 校验和格式转换
- 统一 JSON Schema 作为所有工具的交换媒介
- 每次写入前自动创建 `<file>.bak.<timestamp>` 备份

## Implementation Phases

### Phase 1: DB Migration

**File**: `server/migrations/109_runtime_config_store.up.sql`

- `runtime_config_snapshot` — 原始配置文件快照（per runtime, per config_type, per provider）
- `runtime_config_parsed` — LLM 解析后的统一 schema（per runtime, per config_type, UNIQUE）

### Phase 2: Protocol Types

**File**: `server/pkg/protocol/messages.go`

- `DaemonHeartbeatPendingConfigRead` / `DaemonHeartbeatPendingConfigWrite`
- 扩展 `DaemonHeartbeatAckPayload`

### Phase 3: Daemon ConfigReader

**File**: `server/internal/daemon/runtime_config.go` (new)

四工具的配置文件路径发现 + 读取：
- **Claude**: `~/.claude/settings.json` (hooks+permissions), `~/.claude.json` (MCP), `skills/`, `rules/`, `memory/`, `CLAUDE.md`
- **Codex**: `~/.codex/config.toml` (MCP+hooks+permissions), `skills/`, `rules/`, `memories/`, `AGENTS.md`
- **OpenCode**: `~/.config/opencode/opencode.json` (MCP+permissions), `skills/`, `rules/`, `AGENTS.md`
- **Hermes**: `~/.hermes/config.yaml` (MCP+approval), `skills/`, `hooks/`, `memories/`, `SOUL.md`

复用现有 `local_skills.go` 的 `localSkillRootForProvider` 逻辑。

**Edge cases**:
- 工具未安装：返回 `supported: false`，不报错
- 配置文件不存在（首次安装）：返回空结构，`success: true`
- 文件权限不足：返回 `success: false` + `error_message`
- 大文件（skills/hooks 目录可能有数百文件）：单次读取上限 100 文件，超过时截断并在 warnings 中标注

### Phase 4: Daemon ConfigWriter

**File**: `server/internal/daemon/runtime_config.go` (same file)

- `WriteProviderConfigs()` — 写回原生格式
- `writeBackup()` — `<file>.bak.<YYYYMMDD-HHMMSS>`
- 写锁（`sync.Mutex`）防止并发写入冲突
- 支持备份恢复

### Phase 5: Heartbeat Integration

**File**: `server/internal/handler/daemon.go`

在 `processHeartbeat()` 中新增 config read/write 队列的 probe/claim 逻辑，遵循现有 `pending_local_skills` 模式。

### Phase 6: Server REST API

**File**: `server/internal/handler/runtime_config_api.go` (new)

| 端点 | 用途 |
|---|---|
| `POST /api/runtimes/{rt}/config/read` | 发起配置读取 |
| `GET /api/runtimes/{rt}/config/read/{reqId}` | 轮询读取结果 |
| `GET /api/runtimes/{rt}/config` | 获取最新存储的快照 |
| `PUT /api/runtimes/{rt}/config` | 发起配置写入 |
| `GET /api/runtimes/{rt}/config/diff` | 两个运行时对比 |
| `POST /api/runtimes/{rt}/config/migrate` | 跨运行时迁移 |

Daemon 端：
- `POST /api/daemon/runtimes/{rt}/config-read/{reqId}/result`
- `POST /api/daemon/runtimes/{rt}/config-write/{reqId}/result`

**File**: `server/cmd/server/router.go` — 注册所有路由

### Phase 7: Daemon Processing

**File**: `server/internal/daemon/daemon.go` + `client.go`

- `handleConfigRead` / `handleConfigWrite`
- `ReportConfigReadResult` / `ReportConfigWriteResult`

### Phase 8: LLM Integration

**File**: `server/internal/handler/runtime_config_llm.go` (new)

两种 LLM 调用：
1. **Raw → Unified Schema**（读时）: 原文 + JSON Schema → 结构化 JSON + unknown_keys
2. **Unified Schema → Native**（写时/迁移）: 结构化 JSON → 目标工具格式（JSON/TOML/YAML）

统一 Schema 定义（JSON Schema，硬编码）覆盖七种配置类型。

### Phase 9: Frontend UI

**New files in `packages/views/runtimes/components/`**:

| 组件 | 功能 |
|---|---|
| `runtime-config-section.tsx` | 主面板，provider 选择器，刷新按钮 |
| `config-type-card.tsx` | 每种配置类型卡片，展开/折叠 |
| `config-editor.tsx` | 内联编辑器（JSON/markdown） |
| `config-diff-view.tsx` | 双列对比视图 |
| `config-migration-dialog.tsx` | 迁移向导 |
| `config-backup-list.tsx` | 备份列表 + 恢复 |

**Modified files**:
- `packages/core/api/client.ts` — 6 个新 API 方法
- `packages/core/types/agent.ts` — 新 TypeScript 类型
- `packages/core/runtimes/runtime-config.ts` — 轮询 + query keys
- `runtime-detail.tsx` 或 `runtimes-page.tsx` — 集成配置面板

### Phase 10: Config Diff/Compare

基于统一 schema 做 JSON diff。返回 added / removed / changed 三部分。

### Phase 11: Migration/Copy Flow

用户选源运行时 → 选配置类型 → 选目标运行时 → LLM 序列化为目标格式 → daemon 写入。

### Phase 12: Backup Restoration

`POST /api/daemon/runtimes/{rt}/config/restore` — 从 `.bak` 文件恢复。

### Phase 13: Router Wiring

所有路由在 `router.go` 中注册，区分 daemon 端和用户端。

### Phase 14: Testing

- Go 单元测试：ConfigReader/Writer、Store 接口、HTTP handlers
- TypeScript 单元测试：API 方法、组件渲染
- E2E：使用 agent-browser 无头模式操作 `multica.dqxcj.top`，测试完整读取→查看→编辑→迁移流程

## Verification

1. `go test ./server/internal/daemon/...` — ConfigReader/Writer 测试
2. `go test ./server/internal/handler/...` — API 端点测试
3. agent-browser E2E：
   - 打开 multica.dqxcj.top → 登录 → 运行时页面
   - 选择一个在线运行时 → 点击配置管理 → 点击刷新读取配置
   - 验证 Skills/MCP/Hooks 等配置卡片正常展示
   - 选另一个运行时 → 点击对比 → 验证 diff 视图
   - 点击迁移 → 选目标运行时 → 选配置类型 → 确认 → 验证写回成功
4. 检查目标机器上的 `.bak` 文件是否生成
