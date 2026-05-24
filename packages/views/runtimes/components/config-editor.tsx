"use client";

import { useState } from "react";
import { toast } from "sonner";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import type {
  RuntimeConfigParsed,
  RuntimeConfigType,
  RuntimeConfigFile,
  ProviderConfigs,
} from "@multica/core/types";

interface ConfigEditorProps {
  open: boolean;
  onClose: () => void;
  config: RuntimeConfigParsed;
  runtimeId: string;
  provider: string;
}

function buildSingleConfigWrite(
  provider: string,
  configType: RuntimeConfigType,
  content: Record<string, unknown>,
): ProviderConfigs {
  const file: RuntimeConfigFile = {
    path: "",
    content: JSON.stringify(content),
    file_type: "json",
  };

  const base: ProviderConfigs = {
    provider,
    version: "",
    supported: true,
    skills: [],
    mcp: null,
    hooks: null,
    permissions: null,
    memory: [],
    rules: [],
    instructions: [],
  };

  switch (configType) {
    case "skills":
      base.skills = [file];
      break;
    case "mcp":
      base.mcp = file;
      break;
    case "hooks":
      base.hooks = file;
      break;
    case "permissions":
      base.permissions = file;
      break;
    case "memory":
      base.memory = [file];
      break;
    case "rules":
      base.rules = [file];
      break;
    case "instructions":
      base.instructions = [file];
      break;
  }

  return base;
}

export function ConfigEditor({
  open,
  onClose,
  config,
  runtimeId,
  provider,
}: ConfigEditorProps) {
  const [text, setText] = useState(() =>
    JSON.stringify(config.unified_schema, null, 2),
  );
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      let parsed: Record<string, unknown>;
      try {
        parsed = JSON.parse(text);
      } catch {
        toast.error("Invalid JSON — check syntax and try again.");
        setSaving(false);
        return;
      }

      const configs = buildSingleConfigWrite(provider, config.config_type, parsed);
      await api.initiateConfigWrite(runtimeId, provider, configs);
      toast.success("Config write request submitted.");
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to write config.",
      );
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Edit {config.config_type} config</DialogTitle>
          <DialogDescription>
            Edit the JSON configuration below. Changes will be sent to the
            daemon for persistence.
          </DialogDescription>
        </DialogHeader>

        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          className="h-80 w-full resize-none rounded-md border bg-muted/20 p-3 font-mono text-xs leading-relaxed outline-none ring-1 ring-border focus:ring-1 focus:ring-ring"
          spellCheck={false}
        />

        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? "Saving..." : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
