package daemon

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReadProviderConfigs dispatches to the appropriate config reader
// based on the provider name.
func ReadProviderConfigs(provider string) (*ProviderConfigs, error) {
	switch provider {
	case "claude":
		return readClaudeConfig()
	case "codex":
		return readCodexConfig()
	case "opencode":
		return readOpenCodeConfig()
	case "hermes":
		return readHermesConfig()
	default:
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}
}

// ---------------------------------------------------------------------------
// Provider-specific readers
// ---------------------------------------------------------------------------

func readClaudeConfig() (*ProviderConfigs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	configDir := filepath.Join(home, ".claude")
	cfg := &ProviderConfigs{
		Provider:  "claude",
		Supported: dirExists(configDir),
	}
	if !cfg.Supported {
		return cfg, nil
	}

	cfg.Version = getToolVersion("claude")

	// Skills: ~/.claude/skills/<name>/SKILL.md
	skillRoot, _, _ := localSkillRootForProvider("claude")
	if skillRoot != "" {
		cfg.Skills = readSkillFiles(skillRoot, "markdown")
	}

	// MCP: ~/.claude/.mcp.json, fallback ~/.claude.json
	mcpPath := filepath.Join(configDir, ".mcp.json")
	if fileExists(mcpPath) {
		content, err := readFileContent(mcpPath)
		if err == nil {
			cfg.MCP = &ConfigFile{
				Path:     filepath.ToSlash(mcpPath),
				Content:  content,
				FileType: "json",
			}
		}
	} else {
		homeMCP := filepath.Join(home, ".claude.json")
		if fileExists(homeMCP) {
			content, err := readFileContent(homeMCP)
			if err == nil {
				cfg.MCP = &ConfigFile{
					Path:     filepath.ToSlash(homeMCP),
					Content:  content,
					FileType: "json",
				}
			}
		}
	}

	// Hooks + Permissions: both from ~/.claude/settings.json
	settingsPath := filepath.Join(configDir, "settings.json")
	if fileExists(settingsPath) {
		content, err := readFileContent(settingsPath)
		if err == nil {
			cf := &ConfigFile{
				Path:     filepath.ToSlash(settingsPath),
				Content:  content,
				FileType: "json",
			}
			cfg.Hooks = cf
			cfg.Permissions = cf
		}
	}

	// Rules: ~/.claude/rules/*.md
	cfg.Rules = readMarkdownDir(filepath.Join(configDir, "rules"))

	// Memory: walk ~/.claude/projects/ for memory directories
	cfg.Memory = readMemoryFiles(filepath.Join(configDir, "projects"), ".md")

	// Instructions: ~/.claude/CLAUDE.md
	claudeMDPath := filepath.Join(configDir, "CLAUDE.md")
	if fileExists(claudeMDPath) {
		content, err := readFileContent(claudeMDPath)
		if err == nil {
			cfg.Instructions = []ConfigFile{
				{
					Path:     filepath.ToSlash(claudeMDPath),
					Content:  content,
					FileType: "markdown",
				},
			}
		}
	}

	return cfg, nil
}

func readCodexConfig() (*ProviderConfigs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	// NOTE: this CODEX_HOME resolution duplicates the same logic inside
	// localSkillRootForProvider("codex") in local_skills.go. Extracting a
	// shared helper would require restructuring local_skills.go, so the
	// duplication is accepted for now — it is only ~3 lines.
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}

	cfg := &ProviderConfigs{
		Provider:  "codex",
		Supported: dirExists(codexHome),
	}
	if !cfg.Supported {
		return cfg, nil
	}

	cfg.Version = getToolVersion("codex")

	// Skills: ~/.codex/skills/<name>/SKILL.md
	skillRoot, _, _ := localSkillRootForProvider("codex")
	if skillRoot != "" {
		cfg.Skills = readSkillFiles(skillRoot, "markdown")
	}

	// MCP + Permissions: ~/.codex/config.toml
	configTOML := filepath.Join(codexHome, "config.toml")
	if fileExists(configTOML) {
		content, err := readFileContent(configTOML)
		if err == nil {
			cf := &ConfigFile{
				Path:     filepath.ToSlash(configTOML),
				Content:  content,
				FileType: "toml",
			}
			cfg.MCP = cf
			cfg.Permissions = cf
		}
	}

	// Hooks: ~/.codex/hooks.json (optional)
	hooksPath := filepath.Join(codexHome, "hooks.json")
	if fileExists(hooksPath) {
		content, err := readFileContent(hooksPath)
		if err == nil {
			cfg.Hooks = &ConfigFile{
				Path:     filepath.ToSlash(hooksPath),
				Content:  content,
				FileType: "json",
			}
		}
	}

	// Rules: ~/.codex/rules/*.md
	cfg.Rules = readMarkdownDir(filepath.Join(codexHome, "rules"))

	// Memory: ~/.codex/memories/
	cfg.Memory = readMemoryFiles(filepath.Join(codexHome, "memories"), ".md")

	// Instructions: ~/.codex/AGENTS.md and ~/.codex/instructions.md
	cfg.Instructions = readInstructionsSlice([]string{
		filepath.Join(codexHome, "AGENTS.md"),
		filepath.Join(codexHome, "instructions.md"),
	})

	return cfg, nil
}

func readOpenCodeConfig() (*ProviderConfigs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	configDir := filepath.Join(home, ".config", "opencode")
	cfg := &ProviderConfigs{
		Provider:  "opencode",
		Supported: dirExists(configDir),
	}
	if !cfg.Supported {
		return cfg, nil
	}

	cfg.Version = getToolVersion("opencode")

	// Skills: ~/.config/opencode/skills/<name>/SKILL.md
	skillRoot, _, _ := localSkillRootForProvider("opencode")
	if skillRoot != "" {
		cfg.Skills = readSkillFiles(skillRoot, "markdown")
	}

	// MCP + Permissions: ~/.config/opencode/opencode.json
	configJSON := filepath.Join(configDir, "opencode.json")
	if fileExists(configJSON) {
		content, err := readFileContent(configJSON)
		if err == nil {
			cf := &ConfigFile{
				Path:     filepath.ToSlash(configJSON),
				Content:  content,
				FileType: "json",
			}
			cfg.MCP = cf
			cfg.Permissions = cf
		}
	}

	// Rules: ~/.config/opencode/rules/*.md
	cfg.Rules = readMarkdownDir(filepath.Join(configDir, "rules"))

	// Instructions: ~/.config/opencode/AGENTS.md
	agentsMDPath := filepath.Join(configDir, "AGENTS.md")
	if fileExists(agentsMDPath) {
		content, err := readFileContent(agentsMDPath)
		if err == nil {
			cfg.Instructions = []ConfigFile{
				{
					Path:     filepath.ToSlash(agentsMDPath),
					Content:  content,
					FileType: "markdown",
				},
			}
		}
	}

	return cfg, nil
}

func readHermesConfig() (*ProviderConfigs, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}

	configDir := filepath.Join(home, ".hermes")
	cfg := &ProviderConfigs{
		Provider:  "hermes",
		Supported: dirExists(configDir),
	}
	if !cfg.Supported {
		return cfg, nil
	}

	cfg.Version = getToolVersion("hermes")

	// Skills: ~/.hermes/skills/<name>/SKILL.md
	skillRoot, _, _ := localSkillRootForProvider("hermes")
	if skillRoot != "" {
		cfg.Skills = readSkillFiles(skillRoot, "markdown")
	}

	// MCP + Permissions: ~/.hermes/config.yaml
	configYAML := filepath.Join(configDir, "config.yaml")
	if fileExists(configYAML) {
		content, err := readFileContent(configYAML)
		if err == nil {
			cf := &ConfigFile{
				Path:     filepath.ToSlash(configYAML),
				Content:  content,
				FileType: "yaml",
			}
			cfg.MCP = cf
			cfg.Permissions = cf
		}
	}

	// Hooks: read files from ~/.hermes/hooks/ directory
	hooksDir := filepath.Join(configDir, "hooks")
	if entries, err := os.ReadDir(hooksDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			hookPath := filepath.Join(hooksDir, entry.Name())
			content, err := readFileContent(hookPath)
			if err == nil {
				cfg.Hooks = &ConfigFile{
					Path:     filepath.ToSlash(hookPath),
					Content:  content,
					FileType: detectFileType(hookPath),
				}
				break
			}
		}
	}

	// Memory: ~/.hermes/memories/MEMORY.md and USER.md
	memoriesDir := filepath.Join(configDir, "memories")
	cfg.Memory = readSpecificMemoryFiles(memoriesDir, []string{"MEMORY.md", "USER.md"})

	// Instructions: ~/.hermes/SOUL.md
	soulPath := filepath.Join(configDir, "SOUL.md")
	if fileExists(soulPath) {
		content, err := readFileContent(soulPath)
		if err == nil {
			cfg.Instructions = []ConfigFile{
				{
					Path:     filepath.ToSlash(soulPath),
					Content:  content,
					FileType: "markdown",
				},
			}
		}
	}

	return cfg, nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// readSkillFiles walks root looking for immediate subdirectories that contain
// a SKILL.md file. Each SKILL.md is returned as a single ConfigFile capped
// at maxConfigFilesPerDir and maxConfigFileSize. Dotfile entries are skipped.
func readSkillFiles(root, fileType string) []ConfigFile {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	files := make([]ConfigFile, 0, len(entries))
	for _, entry := range entries {
		if len(files) >= maxConfigFilesPerDir {
			break
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(root, entry.Name(), "SKILL.md")
		content, err := readFileContent(skillPath)
		if err != nil {
			continue
		}

		files = append(files, ConfigFile{
			Path:     filepath.ToSlash(filepath.Join(entry.Name(), "SKILL.md")),
			Content:  content,
			FileType: fileType,
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files
}

// readMarkdownDir reads all .md files in a flat directory. Non-existent or
// non-directory inputs return nil. Results are capped at maxConfigFilesPerDir.
func readMarkdownDir(dir string) []ConfigFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	files := make([]ConfigFile, 0, len(entries))
	for _, entry := range entries {
		if len(files) >= maxConfigFilesPerDir {
			break
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		content, err := readFileContent(path)
		if err != nil {
			continue
		}

		files = append(files, ConfigFile{
			Path:     relativizeHomePath(path),
			Content:  content,
			FileType: "markdown",
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files
}

// readMemoryFiles walks root recursively looking for files whose name ends
// with ext (case-insensitive; pass "" to accept any extension). Results are
// capped at maxConfigFilesPerDir. Dotfile entries (and dotfile directories)
// are skipped.
func readMemoryFiles(root, ext string) []ConfigFile {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}

	var files []ConfigFile
	filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if len(files) >= maxConfigFilesPerDir {
			return filepath.SkipAll
		}

		if ext != "" && !strings.HasSuffix(strings.ToLower(d.Name()), strings.ToLower(ext)) {
			return nil
		}

		content, err := readFileContent(path)
		if err != nil {
			return nil
		}

		rel, _ := filepath.Rel(root, path)
		files = append(files, ConfigFile{
			Path:     filepath.ToSlash(rel),
			Content:  content,
			FileType: detectFileType(path),
		})
		return nil
	})

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files
}

// readSpecificMemoryFiles reads specific filenames from a directory.
// Only files that exist, are non-dotfiles, and are under maxConfigFileSize
// are included.
func readSpecificMemoryFiles(dir string, names []string) []ConfigFile {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}

	var files []ConfigFile
	for _, name := range names {
		if len(files) >= maxConfigFilesPerDir {
			break
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		content, err := readFileContent(path)
		if err != nil {
			continue
		}
		files = append(files, ConfigFile{
			Path:     filepath.ToSlash(path),
			Content:  content,
			FileType: detectFileType(path),
		})
	}

	return files
}

// readInstructionsSlice reads a set of file paths and returns ConfigFiles for
// those that exist and are under the size limit.
func readInstructionsSlice(paths []string) []ConfigFile {
	var files []ConfigFile
	for _, p := range paths {
		content, err := readFileContent(p)
		if err != nil {
			continue
		}
		files = append(files, ConfigFile{
			Path:     filepath.ToSlash(p),
			Content:  content,
			FileType: detectFileType(p),
		})
	}
	return files
}

// ---------------------------------------------------------------------------
// Low-level helpers
// ---------------------------------------------------------------------------

// readFileContent reads a file if it exists and is under maxConfigFileSize.
func readFileContent(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > int64(maxConfigFileSize) {
		return "", fmt.Errorf("file exceeds max size")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// detectFileType returns the file_type label based on file extension.
func detectFileType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "json"
	case ".toml":
		return "toml"
	case ".yaml", ".yml":
		return "yaml"
	case ".md", ".markdown":
		return "markdown"
	default:
		return "text"
	}
}

// getToolVersion runs `<name> --version` with a 5-second timeout and returns
// the trimmed output, or empty string on any failure.
func getToolVersion(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, "--version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
