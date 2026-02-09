package rootio

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "Margin")
		}
		return filepath.Join(home, "AppData", "Roaming", "Margin")
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Margin")
	default:
		return filepath.Join(home, ".local", "share", "margin")
	}
}

func EnsureLayout(root string) error {
	dirs := []string{
		filepath.Join(root, "scratch", "current"),
		filepath.Join(root, "scratch", "history"),
		filepath.Join(root, "inbox"),
		filepath.Join(root, "slack"),
		filepath.Join(root, "index"),
		filepath.Join(root, "bin"),
		filepath.Join(root, "logs"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-margin-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func RelUnderRoot(root, p string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absRoot, absP)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path outside root")
	}
	return filepath.ToSlash(rel), nil
}

func ResolvePathGroups(root string, groups []string) []string {
	if len(groups) == 0 {
		groups = []string{"scratch", "inbox", "slack"}
	}
	out := make([]string, 0, len(groups)+1)
	for _, g := range groups {
		switch strings.TrimSpace(g) {
		case "scratch":
			out = append(out, filepath.Join(root, "scratch", "current"))
			out = append(out, filepath.Join(root, "scratch", "history"))
		case "inbox":
			out = append(out, filepath.Join(root, "inbox"))
		case "slack":
			out = append(out, filepath.Join(root, "slack"))
		}
	}
	return out
}

func ListFilesRecursive(paths []string) ([]string, error) {
	files := make([]string, 0, 128)
	for _, root := range paths {
		st, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !st.IsDir() {
			files = append(files, root)
			continue
		}
		err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			files = append(files, p)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func TimestampSlug(t time.Time) string {
	return t.Format("20060102T150405")
}
