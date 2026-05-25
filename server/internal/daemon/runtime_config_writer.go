package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var configWriteMu sync.Mutex

// WriteProviderConfigs writes config files back to disk with .bak backups.
// Returns paths of created backup files.
func WriteProviderConfigs(configs *ProviderConfigs) ([]string, error) {
	configWriteMu.Lock()
	defer configWriteMu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}

	var backups []string

	writeCfg := func(origPath string, cfg *ConfigFile) error {
		if cfg == nil {
			return nil
		}
		realPath := resolvePath(origPath, home)
		bakPath, err := writeBackup(realPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("backup %s: %w", realPath, err)
		}
		if bakPath != "" {
			backups = append(backups, bakPath)
		}
		if err := os.WriteFile(realPath, []byte(cfg.Content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", realPath, err)
		}
		return nil
	}

	// Write structured config files based on provider
	switch configs.Provider {
	case "claude":
		if err := writeCfg(filepath.Join(home, ".claude", "settings.json"), configs.Hooks); err != nil {
			return backups, err
		}
		if err := writeCfg(filepath.Join(home, ".claude", ".mcp.json"), configs.MCP); err != nil {
			return backups, err
		}
	case "codex":
		codexHome := codexHomeDir(home)
		if err := writeCfg(filepath.Join(codexHome, "config.toml"), configs.MCP); err != nil {
			return backups, err
		}
		if configs.Hooks != nil {
			if err := writeCfg(filepath.Join(codexHome, "hooks.json"), configs.Hooks); err != nil {
				return backups, err
			}
		}
	case "opencode":
		base := filepath.Join(home, ".config", "opencode")
		if err := writeCfg(filepath.Join(base, "opencode.json"), configs.MCP); err != nil {
			return backups, err
		}
	case "hermes":
		base := filepath.Join(home, ".hermes")
		if err := writeCfg(filepath.Join(base, "config.yaml"), configs.MCP); err != nil {
			return backups, err
		}
	}

	return backups, nil
}

// writeBackup creates a timestamped backup of a file before modification.
func writeBackup(src string) (string, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err // let caller handle (file may not exist yet)
	}
	bakPath := src + ".bak." + time.Now().Format("20060102-150405")
	if err := os.WriteFile(bakPath, data, 0644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return bakPath, nil
}

// resolvePath expands ~/ prefixed paths to absolute paths.
func resolvePath(path string, home string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		return filepath.Join(home, path[1:])
	}
	return path
}

// codexHomeDir returns the Codex home directory (respects CODEX_HOME env var).
func codexHomeDir(home string) string {
	if d := strings.TrimSpace(os.Getenv("CODEX_HOME")); d != "" {
		return d
	}
	return filepath.Join(home, ".codex")
}

// RestoreBackup copies a .bak file back to its original path.
func RestoreBackup(bakPath string) error {
	if !strings.Contains(bakPath, ".bak.") {
		return fmt.Errorf("not a backup file: %s", bakPath)
	}
	origPath := strings.SplitN(bakPath, ".bak.", 2)[0]
	data, err := os.ReadFile(bakPath)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	return os.WriteFile(origPath, data, 0644)
}
