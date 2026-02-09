package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type RunBlockConfig struct {
	PythonBin string `json:"python_bin"`
	Shell     string `json:"shell"`
	SQLCmd    string `json:"sql_cmd,omitempty"`
}

type Config struct {
	AutosaveIntervalSeconds int               `json:"autosave_interval_seconds"`
	SnapshotIntervalMinutes int               `json:"snapshot_interval_minutes"`
	SearchPaths             []string          `json:"search_paths"`
	RemindEnabled           bool              `json:"remind_enabled"`
	SlackEnabled            bool              `json:"slack_enabled"`
	MCPEnabled              bool              `json:"mcp_enabled"`
	MCPReadonly             bool              `json:"mcp_readonly"`
	ForceMarkdownExtension  bool              `json:"force_markdown_extension"`
	SyntaxExtensionMap      map[string]string `json:"syntax_extension_map"`
	RunBlock                RunBlockConfig    `json:"runblock"`
}

func Default() Config {
	return Config{
		AutosaveIntervalSeconds: 5,
		SnapshotIntervalMinutes: 10,
		SearchPaths:             []string{"scratch", "inbox", "slack"},
		RemindEnabled:           false,
		SlackEnabled:            false,
		MCPEnabled:              false,
		MCPReadonly:             true,
		ForceMarkdownExtension:  true,
		SyntaxExtensionMap: map[string]string{
			"Plain Text": "md",
			"Markdown":   "md",
			"Python":     "py",
			"JSON":       "json",
			"Shell":      "sh",
		},
		RunBlock: RunBlockConfig{
			PythonBin: "python",
			Shell:     "bash",
		},
	}
}

func Load(root, configPath string) (Config, string, error) {
	cfg := Default()
	if configPath == "" {
		configPath = filepath.Join(root, "config.json")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, configPath, nil
		}
		return cfg, configPath, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, configPath, err
	}
	if cfg.AutosaveIntervalSeconds <= 0 {
		cfg.AutosaveIntervalSeconds = 5
	}
	if cfg.SnapshotIntervalMinutes <= 0 {
		cfg.SnapshotIntervalMinutes = 10
	}
	if len(cfg.SearchPaths) == 0 {
		cfg.SearchPaths = []string{"scratch", "inbox", "slack"}
	}
	if cfg.RunBlock.PythonBin == "" {
		cfg.RunBlock.PythonBin = "python"
	}
	if cfg.RunBlock.Shell == "" {
		cfg.RunBlock.Shell = "bash"
	}
	return cfg, configPath, nil
}
