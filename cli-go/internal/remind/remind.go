package remind

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"margin/internal/rootio"
)

var remindRe = regexp.MustCompile(`REMIND\[([^\]]+)\]\s*(.+)$`)

type Entry struct {
	ID         string `json:"id"`
	When       string `json:"when"`
	Message    string `json:"message"`
	SourcePath string `json:"source_path"`
	SourceLine int    `json:"source_line"`
	Fired      bool   `json:"fired"`
	FiredAt    string `json:"fired_at,omitempty"`
}

type Store struct {
	Entries []Entry `json:"entries"`
}

type ScanResult struct {
	Found int `json:"found"`
	Added int `json:"added"`
	Total int `json:"total"`
}

type ScheduleResult struct {
	Due []Entry `json:"due"`
}

func Scan(root string, includeHistory bool) (ScanResult, error) {
	groups := []string{"scratch", "inbox", "slack"}
	paths := rootio.ResolvePathGroups(root, groups)
	if !includeHistory {
		filtered := make([]string, 0, len(paths))
		for _, p := range paths {
			if strings.HasSuffix(filepath.ToSlash(p), "scratch/history") {
				continue
			}
			filtered = append(filtered, p)
		}
		paths = filtered
	}
	files, err := rootio.ListFilesRecursive(paths)
	if err != nil {
		return ScanResult{}, err
	}
	store, err := loadStore(root)
	if err != nil {
		return ScanResult{}, err
	}
	known := map[string]Entry{}
	for _, e := range store.Entries {
		known[e.ID] = e
	}
	found, added := 0, 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			m := remindRe.FindStringSubmatch(line)
			if len(m) != 3 {
				continue
			}
			when, err := parseWhen(m[1])
			if err != nil {
				continue
			}
			rel, err := rootio.RelUnderRoot(root, f)
			if err != nil {
				rel = filepath.ToSlash(f)
			}
			id := hashID(rel, i+1, when.Format(time.RFC3339), m[2])
			found++
			if _, ok := known[id]; ok {
				continue
			}
			entry := Entry{
				ID:         id,
				When:       when.Format(time.RFC3339),
				Message:    strings.TrimSpace(m[2]),
				SourcePath: rel,
				SourceLine: i + 1,
			}
			store.Entries = append(store.Entries, entry)
			known[id] = entry
			added++
		}
	}
	sort.Slice(store.Entries, func(i, j int) bool { return store.Entries[i].When < store.Entries[j].When })
	if err := saveStore(root, store); err != nil {
		return ScanResult{}, err
	}
	return ScanResult{Found: found, Added: added, Total: len(store.Entries)}, nil
}

func Schedule(root string, notify bool) (ScheduleResult, error) {
	store, err := loadStore(root)
	if err != nil {
		return ScheduleResult{}, err
	}
	now := time.Now()
	due := make([]Entry, 0)
	changed := false
	for i := range store.Entries {
		e := &store.Entries[i]
		if e.Fired {
			continue
		}
		when, err := time.Parse(time.RFC3339, e.When)
		if err != nil {
			continue
		}
		if when.After(now) {
			continue
		}
		e.Fired = true
		e.FiredAt = now.Format(time.RFC3339)
		due = append(due, *e)
		changed = true
		if notify {
			_ = sendNotification(e.Message)
		}
	}
	if changed {
		if err := saveStore(root, store); err != nil {
			return ScheduleResult{}, err
		}
	}
	return ScheduleResult{Due: due}, nil
}

func parseWhen(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == len("2006-01-02") {
		t, err := time.ParseInLocation("2006-01-02", raw, time.Local)
		if err != nil {
			return time.Time{}, err
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 9, 0, 0, 0, time.Local), nil
	}
	return time.ParseInLocation("2006-01-02 15:04", raw, time.Local)
}

func hashID(parts ...any) string {
	h := sha1.New()
	for _, p := range parts {
		_, _ = fmt.Fprint(h, p)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func storePath(root string) string {
	return filepath.Join(root, "index", "reminders.json")
}

func loadStore(root string) (Store, error) {
	p := storePath(root)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Store{}, nil
		}
		return Store{}, err
	}
	var st Store
	if err := json.Unmarshal(data, &st); err != nil {
		return Store{}, err
	}
	return st, nil
}

func saveStore(root string, st Store) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return rootio.AtomicWriteFile(storePath(root), b, 0o644)
}

func sendNotification(msg string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("osascript", "-e", fmt.Sprintf("display notification %q with title \"Margin Reminder\"", msg)).Run()
	case "linux":
		return exec.Command("notify-send", "Margin Reminder", msg).Run()
	case "windows":
		script := fmt.Sprintf("[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null; [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] > $null; $template = [Windows.UI.Notifications.ToastTemplateType]::ToastText02; $xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent($template); $textNodes = $xml.GetElementsByTagName('text'); $textNodes.Item(0).AppendChild($xml.CreateTextNode('Margin Reminder')) > $null; $textNodes.Item(1).AppendChild($xml.CreateTextNode('%s')) > $null; $toast = [Windows.UI.Notifications.ToastNotification]::new($xml); $notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('Margin'); $notifier.Show($toast)", strings.ReplaceAll(msg, "'", "''"))
		return exec.Command("powershell", "-NoProfile", "-Command", script).Run()
	default:
		return nil
	}
}
