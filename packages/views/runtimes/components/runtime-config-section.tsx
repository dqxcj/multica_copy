"use client";

import { useState, useCallback } from "react";
import { FileText, ChevronRight, RefreshCw, Copy, Check, X, ChevronDown } from "lucide-react";
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
interface UnifiedServer { name: string; type: string; command?: string; args?: string[]; url?: string; env?: Record<string,string>; enabled: boolean; }
interface UnifiedHook { event: string; matcher?: string; command: string; timeout?: number; }
interface UnifiedRule { tool: string; pattern?: string; action: string; }
interface UnifiedFile { path: string; content: string; role?: string; }
interface UnifiedSchema {
  skills?: UnifiedSkill[]; servers?: UnifiedServer[]; hooks?: UnifiedHook[];
  rules?: UnifiedRule[]; files?: UnifiedFile[]; approval_policy?: string;
}

type MigrateSelection = Record<string, string[]>;

function summarize(schema: UnifiedSchema): string {
  if (schema.skills) return `${schema.skills.length} 个技能`;
  if (schema.servers) return `${schema.servers.length} 个服务器`;
  if (schema.hooks) return `${schema.hooks.length} 个钩子`;
  if (schema.rules) {
    const a = schema.rules.filter(r=>r.action==="allow").length;
    const d = schema.rules.filter(r=>r.action==="deny").length;
    const q = schema.rules.filter(r=>r.action==="ask").length;
    return `${schema.rules.length} 条规则：${a} 允许，${d} 拒绝，${q} 询问`;
  }
  if (schema.files) return `${schema.files.length} 个文件`;
  return `${Object.keys(schema).length} 字段`;
}

function shortPath(p: string) { return p.split("/").pop() || p; }

// ── main section ──

interface Props { runtimeId: string; provider: string; }

export function RuntimeConfigSection({ runtimeId, provider }: Props) {
  const qc = useQueryClient();
  const { data: configs = [] } = useRuntimeConfigs(runtimeId);
  const [refreshing, setRefreshing] = useState(false);
  const [migrateMode, setMigrateMode] = useState(false);
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

  const toggleItem = (type: string, key: string) => {
    setMigrateSelect((prev) => {
      const arr = prev[type] || [];
      return { ...prev, [type]: arr.includes(key) ? arr.filter(k => k !== key) : [...arr, key] };
    });
  };

  const selectedCount = Object.values(migrateSelect).reduce((s, a) => s + a.length, 0);

  return (
    <div className="space-y-3 rounded-lg border bg-card p-4">
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

      {migrateMode && (
        <div className="rounded border border-dashed border-blue-300 bg-blue-50 p-2 text-xs text-blue-700">
          已选 {selectedCount} 项。勾选要复制的条目，然后点击顶部"复制到其他运行时"
        </div>
      )}

      {configs.length === 0 && !refreshing ? (
        <div className="rounded-lg border border-dashed py-6 text-center">
          <FileText className="mx-auto h-5 w-5 text-muted-foreground/40" />
          <p className="mt-2 text-xs text-muted-foreground">暂无配置数据，点击刷新从运行时拉取</p>
        </div>
      ) : (
        <div className="space-y-2">
          {configs.map((c) => (
            <ConfigCard key={c.id} config={c} migrateMode={migrateMode} migrateSelect={migrateSelect} onToggle={toggleItem} />
          ))}
        </div>
      )}
    </div>
  );
}

// ── card ──

function ConfigCard({ config, migrateMode, migrateSelect, onToggle }: {
  config: { id: string; config_type: string; unified_schema: Record<string, unknown> };
  migrateMode: boolean; migrateSelect: MigrateSelection; onToggle: (t: string, k: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const schema = config.unified_schema as UnifiedSchema;
  const type = config.config_type;

  return (
    <div className="rounded-lg border bg-card">
      <button type="button" onClick={() => setExpanded(v => !v)}
        className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm hover:bg-accent/50">
        <ChevronRight className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition ${expanded ? "rotate-90" : ""}`} />
        <span className="font-medium">{LABELS[type] ?? type}</span>
        <span className="truncate text-xs text-muted-foreground">{summarize(schema)}</span>
      </button>
      {expanded && (
        <div className="border-t px-3 py-2">
          {type === "skills" && schema.skills && <SkillsView skills={schema.skills} migrateMode={migrateMode} selected={migrateSelect.skills||[]} onToggle={k => onToggle("skills",k)} />}
          {type === "mcp" && schema.servers && <ServersView servers={schema.servers} migrateMode={migrateMode} selected={migrateSelect.servers||[]} onToggle={k => onToggle("servers",k)} />}
          {type === "hooks" && schema.hooks && <HooksView hooks={schema.hooks} migrateMode={migrateMode} selected={migrateSelect.hooks||[]} onToggle={k => onToggle("hooks",k)} />}
          {type === "permissions" && schema.rules && <PermissionsView rules={schema.rules} policy={schema.approval_policy} migrateMode={migrateMode} selected={migrateSelect.rules||[]} onToggle={k => onToggle("rules",k)} />}
          {type === "memory" && schema.files && <FilesView files={schema.files} migrateMode={migrateMode} selected={migrateSelect.files||[]} onToggle={k => onToggle("files",k)} />}
          {type === "rules" && schema.files && <FilesView files={schema.files} migrateMode={migrateMode} selected={migrateSelect.files||[]} onToggle={k => onToggle("files",k)} />}
          {type === "instructions" && schema.files && <InstructionsView files={schema.files as UnifiedFile[]} migrateMode={migrateMode} selected={migrateSelect.files||[]} onToggle={k => onToggle("files",k)} />}
          {!schema.skills && !schema.servers && !schema.hooks && !schema.rules && !schema.files && (
            <pre className="whitespace-pre-wrap break-all text-xs">{JSON.stringify(schema, null, 2)}</pre>
          )}
        </div>
      )}
    </div>
  );
}

// ── expandable row helper ──

function ExpandableRow({ summary, detail, indent }: { summary: React.ReactNode; detail: React.ReactNode; indent?: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <tr className="border-b last:border-0 hover:bg-accent/30 cursor-pointer" onClick={() => setOpen(v => !v)}>
        {summary}
      </tr>
      {open && (
        <tr className="bg-muted/30">
          <td colSpan={10} className="px-3 py-2">
            <div className="max-h-96 overflow-auto rounded bg-muted p-2">
              {detail}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

function ToggleBox({ checked, onChange }: { checked: boolean; onChange: () => void }) {
  return (
    <span className="mr-1 inline-flex h-4 w-4 shrink-0 items-center justify-center rounded border cursor-pointer" onClick={(e) => { e.stopPropagation(); onChange(); }}>
      {checked ? <Check className="h-2.5 w-2.5 text-blue-600" /> : null}
    </span>
  );
}

function TypeBadge({ type }: { type: string }) {
  const m: Record<string,string> = { stdio: "bg-blue-100 text-blue-700", sse: "bg-purple-100 text-purple-700", http: "bg-green-100 text-green-700" };
  return <span className={`rounded px-1 py-0.5 text-[10px] font-medium ${m[type]||"bg-gray-100"}`}>{type}</span>;
}

function ActionBadge({ action }: { action: string }) {
  const m: Record<string,string> = { allow: "bg-green-100 text-green-700", deny: "bg-red-100 text-red-700", ask: "bg-amber-100 text-amber-700" };
  const t: Record<string,string> = { allow: "允许", deny: "拒绝", ask: "询问" };
  return <span className={`rounded px-1 py-0.5 text-[10px] font-medium ${m[action]||""}`}>{t[action]||action}</span>;
}

// ── structured views ──

function SkillsView({ skills, migrateMode, selected, onToggle }: { skills: UnifiedSkill[]; migrateMode: boolean; selected: string[]; onToggle: (n: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-5 py-1 text-left">{migrateMode?"✓":""}</th><th className="py-1 text-left">名称</th><th className="py-1 text-left">描述</th><th className="w-10"></th></tr></thead>
      <tbody>
        {skills.map(s => (
          <ExpandableRow key={s.name}
            summary={<>
              <td className="py-1">{migrateMode && <ToggleBox checked={selected.includes(s.name)} onChange={() => onToggle(s.name)} />}</td>
              <td className="py-1 font-medium">{s.name}</td>
              <td className="py-1 text-muted-foreground max-w-[200px] truncate">{s.description || "-"}</td>
              <td className="py-1 text-right"><ChevronDown className="inline h-3 w-3 text-muted-foreground" /></td>
            </>}
            detail={<pre className="whitespace-pre-wrap break-all text-[11px] leading-relaxed">{s.content || "(无正文)"}</pre>}
          />
        ))}
      </tbody>
    </table>
  );
}

function ServersView({ servers, migrateMode, selected, onToggle }: { servers: UnifiedServer[]; migrateMode: boolean; selected: string[]; onToggle: (n: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-5 py-1 text-left">{migrateMode?"✓":""}</th><th className="py-1 text-left">名称</th><th className="py-1 text-left">类型</th><th className="py-1 text-left">命令/URL</th><th className="py-1 text-left">状态</th></tr></thead>
      <tbody>
        {servers.map(s => (
          <ExpandableRow key={s.name}
            summary={<>
              <td className="py-1">{migrateMode && <ToggleBox checked={selected.includes(s.name)} onChange={() => onToggle(s.name)} />}</td>
              <td className="py-1 font-medium">{s.name}</td>
              <td className="py-1"><TypeBadge type={s.type} /></td>
              <td className="py-1 font-mono text-[10px] truncate max-w-[180px]">{s.command || s.url || "-"}</td>
              <td className="py-1">{s.enabled ? <span className="text-green-600">启用</span> : <span className="text-muted-foreground">禁用</span>}</td>
            </>}
            detail={
              <div className="space-y-1 text-[11px]">
                <p><strong>类型：</strong>{s.type}</p>
                {s.command && <p><strong>命令：</strong><code>{s.command}</code></p>}
                {s.args && s.args.length > 0 && <p><strong>参数：</strong><code>{s.args.join(" ")}</code></p>}
                {s.url && <p><strong>URL：</strong><code>{s.url}</code></p>}
                {s.env && Object.keys(s.env).length > 0 && (
                  <details><summary className="cursor-pointer"><strong>环境变量：</strong>{Object.keys(s.env).length} 个</summary>
                    <pre className="mt-1 text-[10px]">{JSON.stringify(s.env, null, 2)}</pre>
                  </details>
                )}
                <p><strong>状态：</strong>{s.enabled ? "启用" : "禁用"}</p>
              </div>
            }
          />
        ))}
      </tbody>
    </table>
  );
}

function HooksView({ hooks, migrateMode, selected, onToggle }: { hooks: UnifiedHook[]; migrateMode: boolean; selected: string[]; onToggle: (c: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-5 py-1 text-left">{migrateMode?"✓":""}</th><th className="py-1 text-left">事件</th><th className="py-1 text-left">匹配</th><th className="py-1 text-left">命令</th></tr></thead>
      <tbody>
        {hooks.map((h,i) => (
          <ExpandableRow key={i}
            summary={<>
              <td className="py-1">{migrateMode && <ToggleBox checked={selected.includes(h.command)} onChange={() => onToggle(h.command)} />}</td>
              <td className="py-1 font-medium">{h.event}</td>
              <td className="py-1 text-muted-foreground">{h.matcher||"-"}</td>
              <td className="py-1 font-mono text-[10px] truncate max-w-[180px]">{h.command}</td>
            </>}
            detail={
              <div className="space-y-1 text-[11px]">
                <p><strong>事件：</strong>{h.event}</p>
                <p><strong>匹配：</strong><code>{h.matcher||"(全部)"}</code></p>
                <p><strong>命令：</strong></p>
                <pre className="whitespace-pre-wrap break-all text-[10px] bg-muted/50 p-1 rounded">{h.command}</pre>
                {h.timeout && <p><strong>超时：</strong>{h.timeout}ms</p>}
              </div>
            }
          />
        ))}
      </tbody>
    </table>
  );
}

function PermissionsView({ rules, policy, migrateMode, selected, onToggle }: { rules: UnifiedRule[]; policy?: string; migrateMode: boolean; selected: string[]; onToggle: (i: string) => void }) {
  return (
    <div>
      {policy && <p className="mb-1 text-xs text-muted-foreground">默认策略：<span className="font-medium">{policy}</span></p>}
      <table className="w-full text-xs">
        <thead><tr className="border-b text-muted-foreground"><th className="w-5 py-1 text-left">{migrateMode?"✓":""}</th><th className="py-1 text-left">工具</th><th className="py-1 text-left">匹配</th><th className="py-1 text-left">操作</th></tr></thead>
        <tbody>
          {rules.map((r, i) => (
            <tr key={i} className="border-b last:border-0 hover:bg-accent/30">
              <td className="py-1">{migrateMode && <ToggleBox checked={selected.includes(String(i))} onChange={() => onToggle(String(i))} />}</td>
              <td className="py-1 font-medium">{r.tool}</td>
              <td className="py-1 text-muted-foreground font-mono text-[10px]">{r.pattern || "-"}</td>
              <td className="py-1"><ActionBadge action={r.action} /></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function FilesView({ files, migrateMode, selected, onToggle }: { files: UnifiedFile[]; migrateMode: boolean; selected: string[]; onToggle: (p: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-5 py-1 text-left">{migrateMode?"✓":""}</th><th className="py-1 text-left">文件</th><th className="py-1 text-left">内容预览</th></tr></thead>
      <tbody>
        {files.map(f => (
          <ExpandableRow key={f.path}
            summary={<>
              <td className="py-1">{migrateMode && <ToggleBox checked={selected.includes(f.path)} onChange={() => onToggle(f.path)} />}</td>
              <td className="py-1 font-medium">{shortPath(f.path)}</td>
              <td className="py-1 text-muted-foreground font-mono text-[10px] truncate max-w-[250px]">{f.content.substring(0, 100)}</td>
            </>}
            detail={<pre className="whitespace-pre-wrap break-all text-[11px] leading-relaxed">{f.content}</pre>}
          />
        ))}
      </tbody>
    </table>
  );
}

function InstructionsView({ files, migrateMode, selected, onToggle }: { files: UnifiedFile[]; migrateMode: boolean; selected: string[]; onToggle: (p: string) => void }) {
  return (
    <table className="w-full text-xs">
      <thead><tr className="border-b text-muted-foreground"><th className="w-5 py-1 text-left">{migrateMode?"✓":""}</th><th className="py-1 text-left">文件</th><th className="py-1 text-left">角色</th><th className="py-1 text-left">内容预览</th></tr></thead>
      <tbody>
        {files.map(f => (
          <ExpandableRow key={f.path}
            summary={<>
              <td className="py-1">{migrateMode && <ToggleBox checked={selected.includes(f.path)} onChange={() => onToggle(f.path)} />}</td>
              <td className="py-1 font-medium">{shortPath(f.path)}</td>
              <td className="py-1 text-muted-foreground">{f.role || "-"}</td>
              <td className="py-1 text-muted-foreground font-mono text-[10px] truncate max-w-[200px]">{f.content.substring(0, 80)}</td>
            </>}
            detail={<pre className="whitespace-pre-wrap break-all text-[11px] leading-relaxed">{f.content}</pre>}
          />
        ))}
      </tbody>
    </table>
  );
}
