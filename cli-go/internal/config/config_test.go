package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultReturnsIndependentCopies(t *testing.T) {
	cfgA := Default()
	cfgA.SearchPaths[0] = "changed"
	cfgA.SyntaxExtensionMap["Markdown"] = "txt"

	cfgB := Default()
	if cfgB.SearchPaths[0] != "scratch" {
		t.Fatalf("unexpected shared SearchPaths: %v", cfgB.SearchPaths)
	}
	if cfgB.SyntaxExtensionMap["Markdown"] != "md" {
		t.Fatalf("unexpected shared SyntaxExtensionMap: %#v", cfgB.SyntaxExtensionMap)
	}
}

func TestLoadAppliesDefaultsForMissingValues(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	if err := os.WriteFile(configPath, []byte(`{"search_paths":[],"runblock":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(root, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AutosaveIntervalSeconds != defaultAutosaveIntervalSeconds {
		t.Fatalf("autosave=%d", cfg.AutosaveIntervalSeconds)
	}
	if cfg.RunBlock.PythonBin != defaultPythonBin {
		t.Fatalf("python_bin=%s", cfg.RunBlock.PythonBin)
	}
	if len(cfg.SearchPaths) == 0 {
		t.Fatal("search paths should be defaulted")
	}
}
