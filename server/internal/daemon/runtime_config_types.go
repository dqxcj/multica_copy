package daemon

const maxConfigFileSize = 1 << 20   // 1MB per file
const maxConfigFilesPerDir = 100    // cap directory listing

// ConfigFile represents a single config file on disk.
type ConfigFile struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	FileType string `json:"file_type"` // "json", "toml", "yaml", "markdown", "text"
}

// ProviderConfigs holds all discovered config files for one provider/tool.
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
