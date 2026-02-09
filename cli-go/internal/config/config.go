package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const (
	defaultAutosaveIntervalSeconds = 5
	defaultSnapshotIntervalMinutes = 10
	defaultPythonBin               = "python"
	defaultShell                   = "bash"
)

var defaultSearchPaths = []string{"scratch", "inbox", "slack"}

var defaultSyntaxExtensionMap = map[string]string{
	"Plain Text": "md",
	"Markdown":   "md",
	"Python":     "py",
	"JSON":       "json",
	"Shell":      "sh",
}

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
		AutosaveIntervalSeconds: defaultAutosaveIntervalSeconds,
		SnapshotIntervalMinutes: defaultSnapshotIntervalMinutes,
		SearchPaths:             cloneStringSlice(defaultSearchPaths),
		RemindEnabled:           false,
		SlackEnabled:            false,
		MCPEnabled:              false,
		MCPReadonly:             true,
		ForceMarkdownExtension:  true,
		SyntaxExtensionMap:      cloneStringMap(defaultSyntaxExtensionMap),
		RunBlock: RunBlockConfig{
			PythonBin: defaultPythonBin,
			Shell:     defaultShell,
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
	cfg.applyDefaults()
	return cfg, configPath, nil
}

func (c *Config) applyDefaults() {
	if c.AutosaveIntervalSeconds <= 0 {
		c.AutosaveIntervalSeconds = defaultAutosaveIntervalSeconds
	}
	if c.SnapshotIntervalMinutes <= 0 {
		c.SnapshotIntervalMinutes = defaultSnapshotIntervalMinutes
	}
	if len(c.SearchPaths) == 0 {
		c.SearchPaths = cloneStringSlice(defaultSearchPaths)
	}
	if c.SyntaxExtensionMap == nil {
		c.SyntaxExtensionMap = cloneStringMap(defaultSyntaxExtensionMap)
	}
	if c.RunBlock.PythonBin == "" {
		c.RunBlock.PythonBin = defaultPythonBin
	}
	if c.RunBlock.Shell == "" {
		c.RunBlock.Shell = defaultShell
	}
}

func cloneStringSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
