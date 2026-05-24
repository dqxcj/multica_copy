package handler

import (
	"encoding/json"
	"fmt"

	toml "github.com/pelletier/go-toml/v2"
	yaml "gopkg.in/yaml.v3"

	"github.com/multica-ai/multica/server/internal/daemon"
)

// ---------------------------------------------------------------------------
// Unified config schemas — the canonical interchange format.
// Every provider's native config is parsed into one of these structures.
// ---------------------------------------------------------------------------

// UnifiedSkills is the canonical skill set.
type UnifiedSkills struct {
	Skills []UnifiedSkill `json:"skills"`
}

// UnifiedSkill is one installable agent capability.
type UnifiedSkill struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Content     string              `json:"content"`
	Files       []UnifiedSkillFile  `json:"files,omitempty"`
}

// UnifiedSkillFile is a supporting file inside a skill directory.
type UnifiedSkillFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// UnifiedMCP is the canonical MCP server list.
type UnifiedMCP struct {
	Servers []UnifiedMCPServer `json:"servers"`
}

// UnifiedMCPServer describes one MCP server.
type UnifiedMCPServer struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // "stdio", "sse", "http"
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	URL     string            `json:"url,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Enabled bool              `json:"enabled"`
}

// UnifiedHooks is the canonical hook list.
type UnifiedHooks struct {
	Hooks []UnifiedHook `json:"hooks"`
}

// UnifiedHook describes one lifecycle hook.
type UnifiedHook struct {
	Event   string `json:"event"`
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// UnifiedPermissions is the canonical permission rules.
type UnifiedPermissions struct {
	Rules          []UnifiedPermissionRule `json:"rules"`
	ApprovalPolicy string                  `json:"approval_policy,omitempty"`
}

// UnifiedPermissionRule is one allow/deny/ask entry.
type UnifiedPermissionRule struct {
	Tool    string `json:"tool"`
	Pattern string `json:"pattern,omitempty"`
	Action  string `json:"action"` // "allow", "deny", "ask"
}

// UnifiedMemory is the canonical memory file set.
type UnifiedMemory struct {
	Files []UnifiedFile `json:"files"`
}

// UnifiedRules is the canonical rule file set.
type UnifiedRules struct {
	Rules []UnifiedRule `json:"rules"`
}

// UnifiedRule is one rules file with optional path scoping.
type UnifiedRule struct {
	Path        string   `json:"path"`
	Content     string   `json:"content"`
	Description string   `json:"description,omitempty"`
	Globs       []string `json:"globs,omitempty"`
}

// UnifiedInstructions is the canonical instruction file set.
type UnifiedInstructions struct {
	Files []UnifiedInstructionFile `json:"files"`
}

// UnifiedInstructionFile is one instruction document.
type UnifiedInstructionFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Role    string `json:"role"` // "system", "user"
}

// UnifiedFile is a generic named content blob.
type UnifiedFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ---------------------------------------------------------------------------
// Parser: raw ConfigFile → unified schema
// ---------------------------------------------------------------------------

// parseUnifiedConfig takes the raw config data from a ConfigReader result
// and converts it to a unified JSON schema based on config type and provider.
// Returns nil if the raw config is empty or unsupported.
func parseUnifiedConfig(ct ConfigType, provider string, rawFiles []daemon.ConfigFile) json.RawMessage {
	switch ct {
	case ConfigTypeSkills:
		return parseUnifiedSkills(rawFiles)
	case ConfigTypeMCP:
		return parseUnifiedMCP(provider, rawFiles)
	case ConfigTypeHooks:
		return parseUnifiedHooks(provider, rawFiles)
	case ConfigTypePermissions:
		return parseUnifiedPermissions(provider, rawFiles)
	case ConfigTypeMemory:
		return parseUnifiedMemory(rawFiles)
	case ConfigTypeRules:
		return parseUnifiedRules(rawFiles)
	case ConfigTypeInstructions:
		return parseUnifiedInstructions(rawFiles)
	}
	return nil
}

// helpers

func marshalUnified(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// parseJSON extracts a JSON object from raw content for the given keys.
// Used for Claude/OpenCode providers where config is JSON.
func parseJSON(raw string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("invalid json: %w", err)
	}
	return m, nil
}

// parseTOML extracts a TOML object into a generic map.
func parseTOML(raw string) (map[string]any, error) {
	var m map[string]any
	if err := toml.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("invalid toml: %w", err)
	}
	return m, nil
}

// parseYAML extracts a YAML object into a generic map.
func parseYAML(raw string) (map[string]any, error) {
	var m map[string]any
	if err := yaml.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("invalid yaml: %w", err)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Skills (provider-agnostic — all are markdown SKILL.md files)
// ---------------------------------------------------------------------------

func parseUnifiedSkills(rawFiles []daemon.ConfigFile) json.RawMessage {
	skills := UnifiedSkills{}
	for _, f := range rawFiles {
		name := ""
		desc := ""
		content := f.Content
		// Try to extract frontmatter name/description from SKILL.md
		if fm := extractFrontmatter(f.Content); fm != nil {
			if n, ok := fm["name"].(string); ok {
				name = n
			}
			if d, ok := fm["description"].(string); ok {
				desc = d
			}
		}
		if name == "" {
			// Derive name from path
			name = skillNameFromPath(f.Path)
		}
		skills.Skills = append(skills.Skills, UnifiedSkill{
			Name:        name,
			Description: desc,
			Content:     content,
		})
	}
	if len(skills.Skills) == 0 {
		return nil
	}
	return marshalUnified(skills)
}

func skillNameFromPath(path string) string {
	// path like "~/.claude/skills/agent-browser/SKILL.md" → "agent-browser"
	parts := splitPath(path)
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "skills" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func splitPath(p string) []string {
	var parts []string
	current := ""
	for _, c := range p {
		if c == '/' || c == '\\' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// Very basic YAML-like frontmatter extractor for SKILL.md files.
// Looks for --- delimited block at the top.
func extractFrontmatter(content string) map[string]any {
	if len(content) < 4 || content[:3] != "---" {
		return nil
	}
	end := indexAfter(content, "\n---", 3)
	if end < 0 {
		return nil
	}
	fm := content[3:end]
	result := make(map[string]any)
	for _, line := range splitLines(fm) {
		line = trimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}
		colon := indexOf(line, ":")
		if colon < 0 {
			continue
		}
		key := trimSpace(line[:colon])
		val := trimSpace(line[colon+1:])
		if key != "" {
			result[key] = val
		}
	}
	return result
}

func indexAfter(s, substr string, start int) int {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else if c == '\r' {
			continue
		} else {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// ---------------------------------------------------------------------------
// MCP — provider-specific parsers
// ---------------------------------------------------------------------------

func parseUnifiedMCP(provider string, rawFiles []daemon.ConfigFile) json.RawMessage {
	if len(rawFiles) == 0 || rawFiles[0].Content == "" {
		return nil
	}
	raw := rawFiles[0].Content

	switch provider {
	case "claude":
		return parseClaudeMCP(raw)
	case "codex":
		return parseCodexMCP(raw)
	case "hermes":
		return parseHermesMCP(raw)
	default:
		return parseClaudeMCP(raw) // JSON fallback
	}
}

func parseClaudeMCP(raw string) json.RawMessage {
	// Claude MCP config: .mcp.json or settings.json mcpServers field
	// Format: { "mcpServers": { "name": { "command":..., "args":... } } }
	// OR from .mcp.json: { "servers": [...] }
	m, err := parseJSON(raw)
	if err != nil {
		return nil
	}

	servers := UnifiedMCP{}

	// Try .mcp.json format first: {"mcpServers": {...}}
	if ms, ok := m["mcpServers"].(map[string]any); ok {
		for name, v := range ms {
			srv := parseClaudeMCPServer(name, v)
			servers.Servers = append(servers.Servers, srv)
		}
	} else {
		// Try settings.json mcpServers key
		for k, v := range m {
			if k == "mcpServers" || k == "mcp" {
				if svrs, ok := v.(map[string]any); ok {
					for name, sv := range svrs {
						srv := parseClaudeMCPServer(name, sv)
						servers.Servers = append(servers.Servers, srv)
					}
				}
			}
		}
	}

	if len(servers.Servers) == 0 {
		return nil
	}
	return marshalUnified(servers)
}

func parseClaudeMCPServer(name string, v any) UnifiedMCPServer {
	srv := UnifiedMCPServer{Name: name, Enabled: true}
	if cfg, ok := v.(map[string]any); ok {
		if cmd, ok := cfg["command"].(string); ok {
			srv.Command = cmd
			srv.Type = "stdio"
		}
		if url, ok := cfg["url"].(string); ok {
			srv.URL = url
			srv.Type = "http"
		}
		if args, ok := cfg["args"].([]any); ok {
			for _, a := range args {
				if as, ok := a.(string); ok {
					srv.Args = append(srv.Args, as)
				}
			}
		}
		if env, ok := cfg["env"].(map[string]any); ok {
			srv.Env = make(map[string]string)
			for ek, ev := range env {
				if es, ok := ev.(string); ok {
					srv.Env[ek] = es
				}
			}
		}
		if disabled, ok := cfg["disabled"].(bool); ok {
			srv.Enabled = !disabled
		}
	}
	return srv
}

func parseCodexMCP(raw string) json.RawMessage {
	// Codex MCP: config.toml [mcp_servers.name] sections
	m, err := parseTOML(raw)
	if err != nil {
		return nil
	}

	servers := UnifiedMCP{}
	mcpServers, ok := m["mcp_servers"].(map[string]any)
	if !ok {
		return nil
	}

	for name, v := range mcpServers {
		srv := UnifiedMCPServer{Name: name, Enabled: true, Type: "stdio"}
		if cfg, ok := v.(map[string]any); ok {
			if cmd, ok := cfg["command"].(string); ok {
				srv.Command = cmd
			}
			if args, ok := cfg["args"].([]any); ok {
				for _, a := range args {
					if as, ok := a.(string); ok {
						srv.Args = append(srv.Args, as)
					}
				}
			}
			if url, ok := cfg["url"].(string); ok {
				srv.URL = url
				srv.Type = "http"
			}
			if env, ok := cfg["env"].(map[string]any); ok {
				srv.Env = make(map[string]string)
				for ek, ev := range env {
					if es, ok := ev.(string); ok {
						srv.Env[ek] = es
					}
				}
			}
			if enabled, ok := cfg["enabled"].(bool); ok {
				srv.Enabled = enabled
			}
			if disabled, ok := cfg["disabled"].(bool); ok {
				srv.Enabled = !disabled
			}
		}
		servers.Servers = append(servers.Servers, srv)
	}

	if len(servers.Servers) == 0 {
		return nil
	}
	return marshalUnified(servers)
}

// ---------------------------------------------------------------------------
// Hooks — provider-specific parsers
// ---------------------------------------------------------------------------

func parseUnifiedHooks(provider string, rawFiles []daemon.ConfigFile) json.RawMessage {
	if len(rawFiles) == 0 || rawFiles[0].Content == "" {
		return nil
	}
	raw := rawFiles[0].Content

	switch provider {
	case "claude":
		return parseClaudeHooks(raw)
	case "codex":
		return parseCodexHooks(raw)
	case "hermes":
		return parseHermesHooks(raw)
	default:
		return nil
	}
}

func parseClaudeHooks(raw string) json.RawMessage {
	m, err := parseJSON(raw)
	if err != nil {
		return nil
	}
	hooksRaw, ok := m["hooks"]
	if !ok {
		return nil
	}

	hooks := UnifiedHooks{}
	// Claude hooks format: { "EventName": [{ "matcher":..., "hooks": [...] }] }
	if hookMap, ok := hooksRaw.(map[string]any); ok {
		for event, entries := range hookMap {
			if arr, ok := entries.([]any); ok {
				for _, entry := range arr {
					if e, ok := entry.(map[string]any); ok {
						matcher, _ := e["matcher"].(string)
						if hookArr, ok := e["hooks"].([]any); ok {
							for _, h := range hookArr {
								if hc, ok := h.(map[string]any); ok {
									cmd, _ := hc["command"].(string)
									hook := UnifiedHook{
										Event:   event,
										Matcher: matcher,
										Command: cmd,
									}
									if t, ok := hc["timeout"].(float64); ok {
										hook.Timeout = int(t)
									}
									hooks.Hooks = append(hooks.Hooks, hook)
								}
							}
						}
					}
				}
			}
		}
	}

	if len(hooks.Hooks) == 0 {
		return nil
	}
	return marshalUnified(hooks)
}

func parseCodexHooks(raw string) json.RawMessage {
	// Codex hooks.json format: array of { "event":..., "matcher":..., "command":... }
	var hookList []map[string]any
	if err := json.Unmarshal([]byte(raw), &hookList); err != nil {
		// Maybe inline TOML hooks — skip for now
		return nil
	}

	hooks := UnifiedHooks{}
	for _, h := range hookList {
		event, _ := h["event"].(string)
		matcher, _ := h["matcher"].(string)
		command, _ := h["command"].(string)
		hook := UnifiedHook{
			Event:   event,
			Matcher: matcher,
			Command: command,
		}
		if t, ok := h["timeout"].(float64); ok {
			hook.Timeout = int(t)
		}
		hooks.Hooks = append(hooks.Hooks, hook)
	}

	if len(hooks.Hooks) == 0 {
		return nil
	}
	return marshalUnified(hooks)
}

// ---------------------------------------------------------------------------
// Permissions — provider-specific parsers
// ---------------------------------------------------------------------------

func parseUnifiedPermissions(provider string, rawFiles []daemon.ConfigFile) json.RawMessage {
	if len(rawFiles) == 0 || rawFiles[0].Content == "" {
		return nil
	}
	raw := rawFiles[0].Content

	switch provider {
	case "claude":
		return parseClaudePermissions(raw)
	case "codex":
		return parseCodexPermissions(raw)
	case "hermes":
		return parseHermesPermissions(raw)
	default:
		return nil
	}
}

func parseClaudePermissions(raw string) json.RawMessage {
	m, err := parseJSON(raw)
	if err != nil {
		return nil
	}
	permsRaw, ok := m["permissions"]
	if !ok {
		return nil
	}

	perms := UnifiedPermissions{}
	if pm, ok := permsRaw.(map[string]any); ok {
		if defMode, ok := pm["defaultMode"].(string); ok {
			perms.ApprovalPolicy = defMode
		}
		// allow list
		if allowList, ok := pm["allow"].([]any); ok {
			for _, a := range allowList {
				if as, ok := a.(string); ok {
					tool, pattern := parsePermissionSpec(as)
					perms.Rules = append(perms.Rules, UnifiedPermissionRule{Tool: tool, Pattern: pattern, Action: "allow"})
				}
			}
		}
		// deny list
		if denyList, ok := pm["deny"].([]any); ok {
			for _, d := range denyList {
				if ds, ok := d.(string); ok {
					tool, pattern := parsePermissionSpec(ds)
					perms.Rules = append(perms.Rules, UnifiedPermissionRule{Tool: tool, Pattern: pattern, Action: "deny"})
				}
			}
		}
		// ask list (anything not in allow/deny)
		// We don't have explicit ask list in Claude — derive it from the ask array if present
		if askList, ok := pm["ask"].([]any); ok {
			for _, a := range askList {
				if as, ok := a.(string); ok {
					tool, pattern := parsePermissionSpec(as)
					perms.Rules = append(perms.Rules, UnifiedPermissionRule{Tool: tool, Pattern: pattern, Action: "ask"})
				}
			}
		}
	}

	if len(perms.Rules) == 0 && perms.ApprovalPolicy == "" {
		return nil
	}
	return marshalUnified(perms)
}

// parsePermissionSpec splits "Read(./.env)" → tool="Read", pattern="./.env"
func parsePermissionSpec(spec string) (tool, pattern string) {
	paren := indexOf(spec, "(")
	if paren < 0 || spec[len(spec)-1] != ')' {
		return spec, ""
	}
	return spec[:paren], spec[paren+1 : len(spec)-1]
}

func parseCodexPermissions(raw string) json.RawMessage {
	m, err := parseTOML(raw)
	if err != nil {
		return nil
	}

	perms := UnifiedPermissions{}

	// approval_policy
	if ap, ok := m["approval_policy"].(string); ok {
		perms.ApprovalPolicy = ap
	}
	// sandbox_mode
	if sm, ok := m["sandbox_mode"].(string); ok {
		perms.Rules = append(perms.Rules, UnifiedPermissionRule{
			Tool:    "sandbox",
			Pattern: sm,
			Action:  "allow",
		})
	}
	// rules files may have separate entries — skip for now as they're markdown

	if len(perms.Rules) == 0 && perms.ApprovalPolicy == "" {
		return nil
	}
	return marshalUnified(perms)
}

// ---------------------------------------------------------------------------
// Memory, Rules, Instructions — mostly markdown files → UnifiedFile lists
// ---------------------------------------------------------------------------

func parseUnifiedMemory(rawFiles []daemon.ConfigFile) json.RawMessage {
	mem := UnifiedMemory{}
	for _, f := range rawFiles {
		if f.Content == "" {
			continue
		}
		mem.Files = append(mem.Files, UnifiedFile{Path: f.Path, Content: f.Content})
	}
	if len(mem.Files) == 0 {
		return nil
	}
	return marshalUnified(mem)
}

func parseUnifiedRules(rawFiles []daemon.ConfigFile) json.RawMessage {
	rules := UnifiedRules{}
	for _, f := range rawFiles {
		if f.Content == "" {
			continue
		}
		rules.Rules = append(rules.Rules, UnifiedRule{
			Path:    f.Path,
			Content: f.Content,
		})
	}
	if len(rules.Rules) == 0 {
		return nil
	}
	return marshalUnified(rules)
}

func parseUnifiedInstructions(rawFiles []daemon.ConfigFile) json.RawMessage {
	inst := UnifiedInstructions{}
	for _, f := range rawFiles {
		if f.Content == "" {
			continue
		}
		role := "system"
		if strContains(f.Path, "AGENTS.md") || strContains(f.Path, "CLAUDE.md") {
			role = "user"
		}
		inst.Files = append(inst.Files, UnifiedInstructionFile{
			Path:    f.Path,
			Content: f.Content,
			Role:    role,
		})
	}
	if len(inst.Files) == 0 {
		return nil
	}
	return marshalUnified(inst)
}

func strContains(s, substr string) bool {
	return indexOf(s, substr) >= 0
}

// ---------------------------------------------------------------------------
// Hermes YAML parsers
// ---------------------------------------------------------------------------

func parseHermesMCP(raw string) json.RawMessage {
	// Hermes MCP: config.yaml mcp_servers key
	m, err := parseYAML(raw)
	if err != nil {
		return nil
	}
	servers := UnifiedMCP{}
	mcpRaw, ok := m["mcp_servers"].(map[string]any)
	if !ok {
		return nil
	}
	for name, v := range mcpRaw {
		srv := UnifiedMCPServer{Name: name, Enabled: true}
		if cfg, ok := v.(map[string]any); ok {
			if cmd, ok := cfg["command"].(string); ok {
				srv.Command = cmd
				srv.Type = "stdio"
			}
			if url, ok := cfg["url"].(string); ok {
				srv.URL = url
				srv.Type = srvType(cfg)
			}
			if args, ok := cfg["args"].([]any); ok {
				for _, a := range args {
					if as, ok := a.(string); ok {
						srv.Args = append(srv.Args, as)
					}
				}
			}
			if env, ok := cfg["env"].(map[string]any); ok {
				srv.Env = make(map[string]string)
				for ek, ev := range env {
					if es, ok := ev.(string); ok {
						srv.Env[ek] = es
					}
				}
			}
			if enabled, ok := cfg["enabled"].(bool); ok {
				srv.Enabled = enabled
			}
		}
		servers.Servers = append(servers.Servers, srv)
	}
	if len(servers.Servers) == 0 {
		return nil
	}
	return marshalUnified(servers)
}

func srvType(cfg map[string]any) string {
	if _, ok := cfg["command"]; ok {
		return "stdio"
	}
	if _, ok := cfg["url"]; ok {
		if ct, ok := cfg["connect_timeout"]; ok || ct != nil {
			return "sse"
		}
		return "http"
	}
	return "stdio"
}

func parseHermesHooks(raw string) json.RawMessage {
	m, err := parseYAML(raw)
	if err != nil {
		return nil
	}
	hooksRaw, ok := m["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	hooks := UnifiedHooks{}
	for event, entries := range hooksRaw {
		if arr, ok := entries.([]any); ok {
			for _, entry := range arr {
				if e, ok := entry.(map[string]any); ok {
					matcher, _ := e["matcher"].(string)
					cmd, _ := e["command"].(string)
					hook := UnifiedHook{Event: event, Matcher: matcher, Command: cmd}
					if t, ok := e["timeout"].(float64); ok {
						hook.Timeout = int(t)
					} else if t, ok := e["timeout"].(int); ok {
						hook.Timeout = t
					}
					hooks.Hooks = append(hooks.Hooks, hook)
				}
			}
		}
	}
	if len(hooks.Hooks) == 0 {
		return nil
	}
	return marshalUnified(hooks)
}

func parseHermesPermissions(raw string) json.RawMessage {
	m, err := parseYAML(raw)
	if err != nil {
		return nil
	}
	perms := UnifiedPermissions{}
	if agent, ok := m["agent"].(map[string]any); ok {
		if ap, ok := agent["approval_policy"].(string); ok {
			perms.ApprovalPolicy = ap
		}
	}
	if allow, ok := m["command_allowlist"].([]any); ok {
		for _, a := range allow {
			if as, ok := a.(string); ok {
				perms.Rules = append(perms.Rules, UnifiedPermissionRule{Tool: as, Action: "allow"})
			}
		}
	}
	if len(perms.Rules) == 0 && perms.ApprovalPolicy == "" {
		return nil
	}
	return marshalUnified(perms)
}

// ---------------------------------------------------------------------------
// Serializer: unified schema → provider native format string
// Used for write/migrate path.
// ---------------------------------------------------------------------------

// serializeUnified converts a unified schema back to a provider-native
// config snippet. Returns "" if the conversion is not supported.
func serializeUnified(ct ConfigType, provider string, unified json.RawMessage) string {
	switch ct {
	case ConfigTypeMCP:
		return serializeMCP(provider, unified)
	case ConfigTypePermissions:
		return serializePermissions(provider, unified)
	default:
		// For file-based types (skills, memory, rules, instructions),
		// the content is already in native format (markdown).
		return ""
	}
}

func serializeMCP(provider string, unified json.RawMessage) string {
	var servers UnifiedMCP
	if err := json.Unmarshal(unified, &servers); err != nil {
		return ""
	}

	switch provider {
	case "claude":
		// Convert to settings.json mcpServers format
		out := map[string]any{}
		for _, s := range servers.Servers {
			entry := map[string]any{}
			switch s.Type {
			case "stdio":
				entry["command"] = s.Command
				if len(s.Args) > 0 {
					entry["args"] = s.Args
				}
			case "http", "sse":
				entry["url"] = s.URL
			}
			if !s.Enabled {
				entry["disabled"] = true
			}
			if len(s.Env) > 0 {
				entry["env"] = s.Env
			}
			out[s.Name] = entry
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return string(b)

	case "codex":
		// Convert to TOML [mcp_servers] format
		var lines []string
		for _, s := range servers.Servers {
			lines = append(lines, fmt.Sprintf("\n[mcp_servers.%s]", s.Name))
			if s.Command != "" {
				lines = append(lines, fmt.Sprintf("command = %q", s.Command))
			}
			if len(s.Args) > 0 {
				lines = append(lines, "args = [")
				for _, a := range s.Args {
					lines = append(lines, fmt.Sprintf("  %q,", a))
				}
				lines = append(lines, "]")
			}
			if s.URL != "" {
				lines = append(lines, fmt.Sprintf("url = %q", s.URL))
			}
			if !s.Enabled {
				lines = append(lines, "enabled = false")
			}
		}
		result := ""
		for _, l := range lines {
			result += l + "\n"
		}
		return result

	default:
		b, _ := json.MarshalIndent(servers, "", "  ")
		return string(b)
	}
}

func serializePermissions(provider string, unified json.RawMessage) string {
	var perms UnifiedPermissions
	if err := json.Unmarshal(unified, &perms); err != nil {
		return ""
	}

	switch provider {
	case "claude":
		out := map[string]any{}
		var allow, deny, ask []string
		for _, r := range perms.Rules {
			spec := r.Tool
			if r.Pattern != "" {
				spec = fmt.Sprintf("%s(%s)", r.Tool, r.Pattern)
			}
			switch r.Action {
			case "allow":
				allow = append(allow, spec)
			case "deny":
				deny = append(deny, spec)
			case "ask":
				ask = append(ask, spec)
			}
		}
		if len(allow) > 0 {
			out["allow"] = allow
		}
		if len(deny) > 0 {
			out["deny"] = deny
		}
		if len(ask) > 0 {
			out["ask"] = ask
		}
		if perms.ApprovalPolicy != "" {
			out["defaultMode"] = perms.ApprovalPolicy
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return string(b)

	case "codex":
		// Convert to TOML
		lines := []string{}
		if perms.ApprovalPolicy != "" {
			lines = append(lines, fmt.Sprintf("approval_policy = %q", perms.ApprovalPolicy))
		}
		for _, r := range perms.Rules {
			if r.Tool == "sandbox" {
				lines = append(lines, fmt.Sprintf("sandbox_mode = %q", r.Pattern))
			}
		}
		result := ""
		for _, l := range lines {
			result += l + "\n"
		}
		return result

	default:
		b, _ := json.MarshalIndent(perms, "", "  ")
		return string(b)
	}
}
