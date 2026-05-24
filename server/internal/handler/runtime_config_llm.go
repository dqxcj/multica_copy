package handler

import (
	"encoding/json"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon"
)

// ConfigType enumerates supported config categories.
type ConfigType string

const (
	ConfigTypeSkills       ConfigType = "skills"
	ConfigTypeMCP          ConfigType = "mcp"
	ConfigTypeHooks        ConfigType = "hooks"
	ConfigTypePermissions  ConfigType = "permissions"
	ConfigTypeMemory       ConfigType = "memory"
	ConfigTypeRules        ConfigType = "rules"
	ConfigTypeInstructions ConfigType = "instructions"
)

// AllConfigTypes is the sorted list of every known ConfigType.
var AllConfigTypes = []ConfigType{
	ConfigTypeSkills, ConfigTypeMCP, ConfigTypeHooks, ConfigTypePermissions,
	ConfigTypeMemory, ConfigTypeRules, ConfigTypeInstructions,
}

// ConfigSchemaUnified holds the LLM-parsed unified representation.
type ConfigSchemaUnified struct {
	ConfigType  ConfigType      `json:"config_type"`
	Provider    string          `json:"provider"`
	Data        json.RawMessage `json:"data"`
	UnknownKeys []string        `json:"unknown_keys,omitempty"`
	Warnings    []string        `json:"warnings,omitempty"`
}

// configTypeFiles returns the raw ConfigFile slice for a given ConfigType.
func configTypeFiles(configs *daemon.ProviderConfigs, ct ConfigType) []daemon.ConfigFile {
	switch ct {
	case ConfigTypeSkills:
		return configs.Skills
	case ConfigTypeMCP:
		if configs.MCP != nil {
			return []daemon.ConfigFile{*configs.MCP}
		}
	case ConfigTypeHooks:
		if configs.Hooks != nil {
			return []daemon.ConfigFile{*configs.Hooks}
		}
	case ConfigTypePermissions:
		if configs.Permissions != nil {
			return []daemon.ConfigFile{*configs.Permissions}
		}
	case ConfigTypeMemory:
		return configs.Memory
	case ConfigTypeRules:
		return configs.Rules
	case ConfigTypeInstructions:
		return configs.Instructions
	}
	return nil
}

// configTypeRaw extracts the raw config content for a given ConfigType
// from the ProviderConfigs. For single-file types it returns the file
// content directly; for multi-file types it concatenates all file contents
// separated by a horizontal rule marker.
func configTypeRaw(configs *daemon.ProviderConfigs, ct ConfigType) string {
	switch ct {
	case ConfigTypeSkills:
		var parts []string
		for _, f := range configs.Skills {
			parts = append(parts, f.Content)
		}
		return strings.Join(parts, "\n---\n")
	case ConfigTypeMCP:
		if configs.MCP != nil {
			return configs.MCP.Content
		}
	case ConfigTypeHooks:
		if configs.Hooks != nil {
			return configs.Hooks.Content
		}
	case ConfigTypePermissions:
		if configs.Permissions != nil {
			return configs.Permissions.Content
		}
	case ConfigTypeMemory:
		var parts []string
		for _, f := range configs.Memory {
			parts = append(parts, f.Content)
		}
		return strings.Join(parts, "\n---\n")
	case ConfigTypeRules:
		var parts []string
		for _, f := range configs.Rules {
			parts = append(parts, f.Content)
		}
		return strings.Join(parts, "\n---\n")
	case ConfigTypeInstructions:
		var parts []string
		for _, f := range configs.Instructions {
			parts = append(parts, f.Content)
		}
		return strings.Join(parts, "\n---\n")
	}
	return ""
}
