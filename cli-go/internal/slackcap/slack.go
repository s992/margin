package slackcap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"margin/internal/rootio"
)

type Message struct {
	User string `json:"user"`
	Text string `json:"text"`
	Ts   string `json:"ts"`
}

type CaptureResult struct {
	SavedPath string         `json:"saved_path"`
	Text      string         `json:"text"`
	Meta      map[string]any `json:"meta"`
}

var (
	headerRe   = regexp.MustCompile(`^\s*(.+?)\s*\[(.+?)\]\s*$`)
	tsPrefixRe = regexp.MustCompile(`^\s*\[(.+?)\]\s*(.*)$`)
)

func Capture(ctx context.Context, root, transcript, format string) (CaptureResult, error) {
	if err := ctx.Err(); err != nil {
		return CaptureResult{}, err
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return CaptureResult{}, errors.New("transcript is required")
	}

	msgs := ParseTranscript(transcript)
	text := renderMessages(msgs, format)
	filename := fmt.Sprintf("%s_%s.md", safeName(firstAuthor(msgs)), time.Now().Format("20060102T150405"))
	saveAbs := filepath.Join(root, "slack", filename)
	if err := rootio.AtomicWriteFile(saveAbs, []byte(text), 0o644); err != nil {
		return CaptureResult{}, err
	}
	rel, err := rootio.RelUnderRoot(root, saveAbs)
	if err != nil {
		rel = filepath.ToSlash(saveAbs)
	}
	return CaptureResult{
		SavedPath: rel,
		Text:      text,
		Meta: map[string]any{
			"source":        "pasted_transcript",
			"message_count": len(msgs),
		},
	}, nil
}

func ParseTranscript(transcript string) []Message {
	lines := strings.Split(strings.ReplaceAll(transcript, "\r\n", "\n"), "\n")
	out := make([]Message, 0, 16)
	var cur *Message

	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(cur.Text)
		if cur.Text != "" {
			out = append(out, *cur)
		}
		cur = nil
	}

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := headerRe.FindStringSubmatch(line); len(m) == 3 {
			flush()
			cur = &Message{User: strings.TrimSpace(m[1]), Ts: strings.TrimSpace(m[2])}
			continue
		}
		if m := tsPrefixRe.FindStringSubmatch(line); len(m) == 3 {
			ts := strings.TrimSpace(m[1])
			text := strings.TrimSpace(m[2])
			if cur == nil {
				cur = &Message{User: "unknown", Ts: ts}
			} else if cur.Ts != ts {
				user := cur.User
				flush()
				cur = &Message{User: user, Ts: ts}
			}
			if text != "" {
				if cur.Text != "" {
					cur.Text += "\n"
				}
				cur.Text += text
			}
			continue
		}
		if cur == nil {
			cur = &Message{User: "unknown", Ts: "unknown"}
		}
		if cur.Text != "" {
			cur.Text += "\n"
		}
		cur.Text += strings.TrimSpace(line)
	}
	flush()
	return out
}

func renderMessages(msgs []Message, format string) string {
	capturedAt := time.Now().Format(time.RFC3339)
	if format == "text" {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("source=pasted_transcript captured_at=%s\n\n", capturedAt))
		for _, m := range msgs {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", m.Ts, m.User, strings.TrimSpace(m.Text)))
		}
		return sb.String()
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Imported conversation** source=slack pasted_text captured_at=%s\n\n", capturedAt))
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("- `%s` **%s**:\n", m.Ts, m.User))
		for _, line := range strings.Split(strings.TrimSpace(m.Text), "\n") {
			sb.WriteString("  " + line + "\n")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func firstAuthor(msgs []Message) string {
	for _, m := range msgs {
		if s := safeName(m.User); s != "" && s != "unknown" {
			return s
		}
	}
	return "slack"
}

func safeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}
