"use client";

import { useCallback, useState } from "react";
import { FileText, RefreshCw } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  useRuntimeConfigs,
  runtimeConfigKeys,
} from "@multica/core/runtimes/runtime-config";
import { Button } from "@multica/ui/components/ui/button";
import { ConfigTypeCard } from "./config-type-card";

interface RuntimeConfigSectionProps {
  runtimeId: string;
  provider: string;
}

export function RuntimeConfigSection({
  runtimeId,
  provider,
}: RuntimeConfigSectionProps) {
  const qc = useQueryClient();
  const { data: configs = [], isLoading } = useRuntimeConfigs(runtimeId);
  const [refreshing, setRefreshing] = useState(false);

  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      const { id } = await api.initiateConfigRead(runtimeId, provider);
      // Poll every 2s until the daemon finishes reading.
      while (true) {
        await new Promise((r) => setTimeout(r, 2000));
        const result = await api.getConfigReadResult(runtimeId, id);
        if (result.status === "completed" || result.status === "failed") {
          break;
        }
      }
      await qc.invalidateQueries({
        queryKey: runtimeConfigKeys.forRuntime(runtimeId),
      });
    } finally {
      setRefreshing(false);
    }
  }, [runtimeId, provider, qc]);

  return (
    <div className="space-y-3 rounded-lg border bg-card p-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h3 className="flex items-center gap-2 text-sm font-semibold">
          <FileText className="h-4 w-4 text-muted-foreground" />
          Agent Configs
        </h3>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={handleRefresh}
          disabled={refreshing}
        >
          <RefreshCw
            className={`h-3 w-3 ${refreshing ? "animate-spin" : ""}`}
          />
          Refresh
        </Button>
      </div>

      {/* Content */}
      {isLoading ? (
        <div className="py-4 text-center text-xs text-muted-foreground">
          Loading configs...
        </div>
      ) : configs.length === 0 ? (
        <div className="rounded-lg border border-dashed py-6 text-center">
          <FileText className="mx-auto h-5 w-5 text-muted-foreground/40" />
          <p className="mt-2 text-xs text-muted-foreground">
            No config data yet. Click Refresh to fetch from daemon.
          </p>
        </div>
      ) : (
        <div className="space-y-2">
          {configs.map((config) => (
            <ConfigTypeCard
              key={config.id}
              config={config}
              runtimeId={runtimeId}
              provider={provider}
            />
          ))}
        </div>
      )}
    </div>
  );
}
