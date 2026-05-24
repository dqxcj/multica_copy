"use client";

import { useState, useCallback } from "react";
import { FileText, ChevronRight, RefreshCw, Copy, Check, X } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useRuntimeConfigs, runtimeConfigKeys } from "@multica/core/runtimes/runtime-config";
import { Button } from "@multica/ui/components/ui/button";

const LABELS: Record<string, string> = {
  skills: "技能", mcp: "MCP 服务器", hooks: "钩子",
  permissions: "权限规则", memory: "记忆", rules: "规则",
  instructions: "指令",
};

interface UnifiedSkill { name: string; description?: string; content?: string; files?: { path: string; content: string }[]; }
interface UnifiedServer { name: string; type: string; command?: string; url?: string; args?: string[]; env?: Record<string,string>; enabled: boolean; }
interface UnifiedHook { event: string; matcher?: string; command: string; timeout?: number; }
interface UnifiedRule { tool: string; pattern?: string; action: string; }
interface UnifiedFile { path: string; content: string; role?: string; }
interface UnifiedSchema {
  skills?: UnifiedSkill[]; servers?: UnifiedServer[]; hooks?: UnifiedHook[];
  rules?: UnifiedRule[]; files?: UnifiedFile[]; approval_policy?: string;
}

// ── helpers ──

function summarize(schema: UnifiedSchema): string {
  if (schema.skills) return `${schema.skills.length} 个技能`;
  if (schema.servers) return `${schema.servers.length} 个服务器`;
  if (schema.hooks) return `${schema.hooks.length} 个钩子`;
  if (schema.rules) {
    const allow = schema.rules.filter(r=>r.action==="allow").length;
    const deny = schema.rules.filter(r=>r.action==="deny").length;
    const ask = schema.rules.filter(r=>r.action==="ask").length;
    const parts = [`${allow} 允许`, `${deny} 拒绝`, `${ask} 询问`];
    return `${schema.rules.length} 条规则：${parts.join("，")}`;
  }
  if (schema.files) return `${schema.files.length} 个文件`;
  return `${Object.keys(schema).length} 字段`;
}

function shortPath(p: string) { return p.split("/").pop() || p; }

// ── item-level migration state ──

interface MigrateSelection {
  skills?: string[];        // skill names
  servers?: string[];       // server names
  hooks?: string[];         // hook commands
  rules?: number[];         // rule indices
  files?: string[];         // file paths
}

// ── main component ──

interface Props { runtimeId: string; provider: string; }

export function RuntimeConfigSection({ runtimeId, provider }: Props) {
  const qc = useQueryClient();
  const { data: configs = [] } = useRuntimeConfigs(runtimeId);
  const [refreshing, setRefreshing] = useState(false);
  const [migrateMode, setMigrateMode] = useState(false);
  const [migrating, setMigrating] = useState(false);
  const [migrateSelect, setMigrateSelect] = useState<MigrateSelection>({});

  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      const { id } = await api.initiateConfigRead(runtimeId, provider);
      while (true) {
        await new Promise((r) => setTimeout(r, 2000));
        const result = await api.getConfigReadResult(runtimeId, id);
        if (result.status === "completed" || result.status === "failed") break;
      }
      qc.invalidateQueries({ queryKey: runtimeConfigKeys.forRuntime(runtimeId) });
    } finally { setRefreshing(false); }
  }, [runtimeId, provider, qc]);

  const toggleMigrateItem = (type: string, key: string) => {
    setMigrateSelect((prev) => {
      const arr = (prev as any)[type] || [];
      const next = arr.includes(key) ? arr.filter((k: string) => k !== key) : [...arr, key];
      return { ...prev, [type]: next };
    });
  };

  const selectedCount = Object.values(migrateSelect).reduce((s, a) => s + a.length, 0);

  return (
    <div className="space-y-3 rounded-lg border bg-card p-4">
      {/* 标题栏 */}
      <div className="flex items-center justify-between">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <FileText className="h-4 w-4 text-muted-foreground" />
          智能体配置
        </h3>
        <div className="flex items-center gap-1">
          <Button type="button" variant="outline" size="sm" onClick={handleRefresh} disabled={refreshing}>
            <RefreshCw className={`h-3 w-3 ${refreshing ? "animate-spin" : ""}`} />
            刷新
          </Button>
          {!migrateMode ? (
            <Button type="button" variant="outline" size="sm" onClick={() => setMigrateMode(true)}>
              <Copy className="h-3 w-3" /> 迁移
            </Button>
          ) : (
            <Button type="button" variant="ghost" size="sm" onClick={() => { setMigrateMode(false); setMigrateSelect({}); }}>
              <X className="h-3 w-3" /> 取消
            </Button>
          )}
        </div>
      </div>

      {/* 迁移提示 */}
      {migrateMode && (
        <div className="rounded border border-dashed border-blue-300 bg-blue-50 p-2 text-xs text-blue-700">
          选择要复制的配置项 ({selectedCount} 项已选)，然后点击底部"复制到其他运行时"
        </div>
      )}

      {/* 空状态 */}
      {configs.length === 0 && !refreshing && (
        <div className="rounded-lg border border-dashed py-6 text-center">
          <FileText className="mx-auto h-5 w-5 text-muted-foreground/40" />
          <p className="mt-2 text-xs text-muted-foreground">暂无配置数据，点击刷新从运行时拉取</p>
        </div>
      )}

      {/* 配置卡片 */}
      <div className="space-y-2">
        {configs.map((c) => (
          <ConfigCard
            key={c.id}
            config={c}
            migrateMode={migrateMode}
            migrateSelect={migrateSelect}
            onToggle={toggleMigrateItem}
          />
        ))}
      </div>
    </div>
  );
}

// ── single config card with structured display ──

function ConfigCard({ config, migrateMode, migrateSelect, onToggle }: {
  config: { id: string; config_type: string; unified_schema: Record<string, unknown> };
  migrateMode: boolean;
  migrateSelect: MigrateSelection;
  onToggle: (type: string, key: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const schema = config.unified_schema as UnifiedSchema;
  const label = LABELS[config.config_type] ?? config.config_type;
  const type = config.config_type;

  return (
    <div className="rounded-lg border bg-card">
      <button type="button" onClick={() => setExpanded((v) => !v)}
        className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/50">
        <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition ${expanded ? "rotate-90" : ""}`} />
        <span className="font-medium">{label}</span>
        <span className="truncate text-xs text-muted-foreground">{summarize(schema)}</span>
      </button>

      {expanded && (
        <div className="border-t px-3 py-2 text-xs">
          {type === "skills" && schema.skills && <SkillsTable skills={schema.skills} migrateMode={migrateMode} selected={migrateSelect.skills || []} onToggle={(k) => onToggle("skills", k)} />}
          {type === "mcp" && schema.servers && <ServersTable servers={schema.servers} migrateMode={migrateMode} selected={migrateSelect.servers || []} onToggle={(k) => onToggle("servers", k)} />}
          {type === "hooks" && schema.hooks && <HooksTable hooks={schema.hooks} migrateMode={migrateMode} selected={migrateSelect.hooks || []} onToggle={(k) => onToggle("hooks", k)} />}
          {type === "permissions" && schema.rules && <PermissionsTable rules={schema.rules} policy={schema.approval_policy} migrateMode={migrateMode} selected={(migrateSelect.rules || []).map(String)} onToggle={(k) => onToggle("rules", k)} />}
          {type === "memory" && schema.files && <FilesTable files={schema.files} migrateMode={migrateMode} selected={migrateSelect.files || []} onToggle={(k) => onToggle("files", k)} />}
          {type === "rules" && schema.files && <FilesTable files={schema.files} migrateMode={migrateMode} selected={migrateSelect.files || []} onToggle={(k) => onToggle("files", k)} />}
          {type === "instructions" && schema.files && <InstructionsTable files={schema.files} migrateMode={migrateMode} selected={migrateSelect.files || []} onToggle={(k) => onToggle("files", k)} />}
          {!schema.skills && !schema.servers && !schema.hooks && !schema.rules && !schema.files && (
            <pre className="whitespace-pre-wrap break-all">{JSON.stringify(schema, null, 2)}</pre>
          )}
        </div>
      )}
    </div>
  );
}

// ── structured sub-components ──

function CheckCell({ checked, onChange, disabled }: { checked: boolean; onChange: () => void; disabled?: boolean }) {
  if (!disabled) return null;
  return (
    <span className="mr-1 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border cursor-pointer" onClick={onChange}>
      {checked ? <Check className="h-2.5 w-2.5 text-blue-600" /> : null}
    </span>
  );
}

function SkillsTable({ skills, migrateMode, selected, onToggle }: { skills: UnifiedSkill[]; migrateMode: boolean; selected: string[]; onToggle: (name: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-4 py-1 text-left">{migrateMode ? "✓" : ""}</th><th className="py-1 text-left">名称</th><th className="py-1 text-left">描述</th></tr></thead>
      <tbody>
        {skills.map((s) => (
          <tr key={s.name} className="border-b last:border-0 hover:bg-accent/30">
            <td className="py-1">{migrateMode ? <CheckCell checked={selected.includes(s.name)} onChange={() => onToggle(s.name)} disabled={true} /> : null}</td>
            <td className="py-1 font-medium">{s.name}</td>
            <td className="py-1 text-muted-foreground">{s.description?.substring(0, 80) || "-"}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function ServersTable({ servers, migrateMode, selected, onToggle }: { servers: UnifiedServer[]; migrateMode: boolean; selected: string[]; onToggle: (name: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-4 py-1 text-left">{migrateMode ? "✓" : ""}</th><th className="py-1 text-left">名称</th><th className="py-1 text-left">类型</th><th className="py-1 text-left">命令/URL</th><th className="py-1 text-left">状态</th></tr></thead>
      <tbody>
        {servers.map((s) => (
          <tr key={s.name} className="border-b last:border-0 hover:bg-accent/30">
            <td className="py-1">{migrateMode ? <CheckCell checked={selected.includes(s.name)} onChange={() => onToggle(s.name)} disabled={true} /> : null}</td>
            <td className="py-1 font-medium">{s.name}</td>
            <td className="py-1"><TypeBadge type={s.type} /></td>
            <td className="py-1 font-mono text-[10px]">{s.command || s.url || "-"}</td>
            <td className="py-1">{s.enabled ? <span className="text-green-600">启用</span> : <span className="text-muted-foreground">禁用</span>}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function TypeBadge({ type }: { type: string }) {
  const colors: Record<string, string> = { stdio: "bg-blue-100 text-blue-700", sse: "bg-purple-100 text-purple-700", http: "bg-green-100 text-green-700" };
  return <span className={`rounded px-1 py-0.5 text-[10px] font-medium ${colors[type] || "bg-gray-100"}`}>{type}</span>;
}

function HooksTable({ hooks, migrateMode, selected, onToggle }: { hooks: UnifiedHook[]; migrateMode: boolean; selected: string[]; onToggle: (cmd: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-4 py-1 text-left">{migrateMode ? "✓" : ""}</th><th className="py-1 text-left">事件</th><th className="py-1 text-left">匹配</th><th className="py-1 text-left">命令</th></tr></thead>
      <tbody>
        {hooks.map((h, i) => (
          <tr key={i} className="border-b last:border-0 hover:bg-accent/30">
            <td className="py-1">{migrateMode ? <CheckCell checked={selected.includes(h.command)} onChange={() => onToggle(h.command)} disabled={true} /> : null}</td>
            <td className="py-1 font-medium">{h.event}</td>
            <td className="py-1 text-muted-foreground">{h.matcher || "-"}</td>
            <td className="py-1 font-mono text-[10px] truncate max-w-[200px]">{h.command}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function PermissionsTable({ rules, policy, migrateMode, selected, onToggle }: { rules: UnifiedRule[]; policy?: string; migrateMode: boolean; selected: string[]; onToggle: (idx: string) => void }) {
  const actionColors: Record<string, string> = { allow: "bg-green-100 text-green-700", deny: "bg-red-100 text-red-700", ask: "bg-amber-100 text-amber-700" };
  return (
    <div>
      {policy && <p className="mb-1 text-muted-foreground">默认策略：<span className="font-medium">{policy}</span></p>}
      <table className="w-full text-xs">
        <thead><tr className="border-b text-muted-foreground"><th className="w-4 py-1 text-left">{migrateMode ? "✓" : ""}</th><th className="py-1 text-left">工具</th><th className="py-1 text-left">匹配</th><th className="py-1 text-left">操作</th></tr></thead>
        <tbody>
          {rules.map((r, i) => (
            <tr key={i} className="border-b last:border-0 hover:bg-accent/30">
              <td className="py-1">{migrateMode ? <CheckCell checked={selected.includes(String(i))} onChange={() => onToggle(String(i))} disabled={true} /> : null}</td>
              <td className="py-1 font-medium">{r.tool}</td>
              <td className="py-1 text-muted-foreground font-mono text-[10px]">{r.pattern || "-"}</td>
              <td className="py-1"><span className={`rounded px-1 py-0.5 text-[10px] font-medium ${actionColors[r.action] || ""}`}>{r.action === "allow" ? "允许" : r.action === "deny" ? "拒绝" : r.action === "ask" ? "询问" : r.action}</span></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function FilesTable({ files, migrateMode, selected, onToggle }: { files: UnifiedFile[]; migrateMode: boolean; selected: string[]; onToggle: (path: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-4 py-1 text-left">{migrateMode ? "✓" : ""}</th><th className="py-1 text-left">文件</th><th className="py-1 text-left">内容预览</th></tr></thead>
      <tbody>
        {files.map((f) => (
          <tr key={f.path} className="border-b last:border-0 hover:bg-accent/30">
            <td className="py-1">{migrateMode ? <CheckCell checked={selected.includes(f.path)} onChange={() => onToggle(f.path)} disabled={true} /> : null}</td>
            <td className="py-1 font-medium">{shortPath(f.path)}</td>
            <td className="py-1 text-muted-foreground font-mono text-[10px] truncate max-w-[300px]">{f.content.substring(0, 100)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function InstructionsTable({ files, migrateMode, selected, onToggle }: { files: UnifiedFile[]; migrateMode: boolean; selected: string[]; onToggle: (path: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-4 py-1 text-left">{migrateMode ? "✓" : ""}</th><th className="py-1 text-left">文件</th><th className="py-1 text-left">角色</th><th className="py-1 text-left">内容预览</th></tr></thead>
      <tbody>
        {files.map((f) => (
          <tr key={f.path} className="border-b last:border-0 hover:bg-accent/30">
            <td className="py-1">{migrateMode ? <CheckCell checked={selected.includes(f.path)} onChange={() => onToggle(f.path)} disabled={true} /> : null}</td>
            <td className="py-1 font-medium">{shortPath(f.path)}</td>
            <td className="py-1 text-muted-foreground">{f.role || "-"}</td>
            <td className="py-1 text-muted-foreground font-mono text-[10px] truncate max-w-[250px]">{f.content.substring(0, 80)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
