"use client";

import { useState } from "react";
import { ChevronRight, AlertTriangle } from "lucide-react";
// inline to avoid bundle issue
type RuntimeConfigType = "skills" | "mcp" | "hooks" | "permissions" | "memory" | "rules" | "instructions";
type RuntimeConfigParsed = { id: string; config_type: RuntimeConfigType; unified_schema: Record<string,unknown>; unknown_keys: string[] };

const CONFIG_TYPE_LABELS: Record<RuntimeConfigType, string> = {
  skills: "Skills",
  mcp: "MCP Config",
  hooks: "Hooks",
  permissions: "Permissions",
  memory: "Memory",
  rules: "Rules",
  instructions: "Instructions",
};

interface ConfigTypeCardProps {
  config: RuntimeConfigParsed;
  runtimeId: string;
  provider: string;
}

export function ConfigTypeCard({ config, runtimeId, provider }: ConfigTypeCardProps) {
  const [expanded, setExpanded] = useState(false);
  // TEMP: const [editorOpen, setEditorOpen] = useState(false);

  const label = CONFIG_TYPE_LABELS[config.config_type] ?? config.config_type;
  const itemCount = Object.keys(config.unified_schema).length;
  const hasUnknownKeys = config.unknown_keys.length > 0;

  return (
    <>
      <div className="rounded-lg border bg-card">
        {/* Header row — always visible, clickable to expand */}
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm transition-colors hover:bg-accent/50"
        >
          <ChevronRight
            className={`h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform ${
              expanded ? "rotate-90" : ""
            }`}
          />
          <span className="font-medium">{label}</span>
          <span className="font-mono text-xs tabular-nums text-muted-foreground">
            {itemCount}
          </span>
          {hasUnknownKeys && (
            <span className="inline-flex items-center gap-1 rounded-md bg-warning/10 px-1.5 py-0.5 text-[11px] font-medium text-warning">
              <AlertTriangle className="h-3 w-3" />
              {config.unknown_keys.length} unknown
            </span>
          )}
          {/* TEMP: <div className="ml-auto">
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              onClick={(e) => {
                e.stopPropagation();
                setEditorOpen(true);
              }}
              title="Edit config"
            >
              <Pencil className="h-3 w-3" />
            </Button>
          </div> */}
        </button>

        {/* Expanded content — scrollable pre */}
        {expanded && (
          <div className="max-h-80 overflow-auto border-t bg-muted/20 px-3 py-2">
            <pre className="whitespace-pre-wrap break-all font-mono text-[11px] leading-relaxed text-foreground">
              {JSON.stringify(config.unified_schema, null, 2)}
            </pre>
          </div>
        )}
      </div>

{/* TEMP:
      <ConfigEditor
        open={editorOpen}
        onClose={() => setEditorOpen(false)}
        config={config}
        runtimeId={runtimeId}
        provider={provider}
      /> */}
    </>
  );
}
