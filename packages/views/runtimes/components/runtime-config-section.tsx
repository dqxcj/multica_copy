"use client";

import { useState } from "react";
import { FileText, ChevronRight } from "lucide-react";
import { useRuntimeConfigs } from "@multica/core/runtimes/runtime-config";

interface RuntimeConfigSectionProps {
  runtimeId: string;
  provider: string;
}

export function RuntimeConfigSection({
  runtimeId,
  provider: _provider,
}: RuntimeConfigSectionProps) {
  const { data: configs = [] } = useRuntimeConfigs(runtimeId);

  return (
    <div className="space-y-3 rounded-lg border bg-card p-4">
      <div className="flex items-center justify-between">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <FileText className="h-4 w-4 text-muted-foreground" />
          Agent Configs
        </h3>
      </div>
      {configs.length === 0 ? (
        <p className="text-xs text-muted-foreground">No config data yet.</p>
      ) : (
        <div className="space-y-2">
          {configs.map((config) => (
            <InlineCard key={config.id} config={config} />
          ))}
        </div>
      )}
    </div>
  );
}

function InlineCard({ config }: { config: any }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <div className="rounded-lg border bg-card">
      <button type="button" onClick={() => setExpanded((v: boolean) => !v)}
        className="flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm">
        <ChevronRight className={`h-3.5 w-3.5 ${expanded ? "rotate-90" : ""}`} />
        <span className="font-medium">{config.config_type}</span>
        <span className="text-xs text-muted-foreground">{Object.keys(config.unified_schema).length}</span>
      </button>
      {expanded && (
        <div className="max-h-80 overflow-auto border-t px-3 py-2">
          <pre className="text-[11px]">{JSON.stringify(config.unified_schema, null, 2)}</pre>
        </div>
      )}
    </div>
  );
}
