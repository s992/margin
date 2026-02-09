package slackcap

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

type apiResp struct {
	OK               bool      `json:"ok"`
	Error            string    `json:"error"`
	Messages         []Message `json:"messages"`
	HasMore          bool      `json:"has_more"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor"`
	} `json:"response_metadata"`
}

var msgURLRe = regexp.MustCompile(`archives/([A-Z0-9]+)/p(\d{16})`)

func ParseThreadInput(channel, thread string) (string, string, error) {
	thread = strings.TrimSpace(thread)
	if strings.Contains(thread, "slack.com") {
		u, err := url.Parse(thread)
		if err != nil {
			return "", "", err
		}
		m := msgURLRe.FindStringSubmatch(u.Path)
		if len(m) == 3 {
			channel = m[1]
			thread = fmt.Sprintf("%s.%s", m[2][:10], strings.TrimLeft(m[2][10:], "0"))
			if strings.HasSuffix(thread, ".") {
				thread += "0"
			}
			return channel, thread, nil
		}
		q := u.Query()
		if c, ok := q["channel"]; ok && len(c) > 0 {
			channel = c[0]
		}
		if t, ok := q["thread_ts"]; ok && len(t) > 0 {
			thread = t[0]
		}
	}
	if channel == "" || thread == "" {
		return "", "", errors.New("channel and thread are required")
	}
	return channel, thread, nil
}

func Capture(root, channel, thread, tokenEnv, format string) (CaptureResult, error) {
	if tokenEnv == "" {
		tokenEnv = "SLACK_TOKEN"
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return CaptureResult{}, fmt.Errorf("missing token in env %s", tokenEnv)
	}
	ch, th, err := ParseThreadInput(channel, thread)
	if err != nil {
		return CaptureResult{}, err
	}
	if !strings.HasPrefix(ch, "C") && !strings.HasPrefix(ch, "G") {
		ch, err = resolveChannelID(ch, token)
		if err != nil {
			return CaptureResult{}, err
		}
	}
	msgs, err := fetchReplies(ch, th, token)
	if err != nil {
		return CaptureResult{}, err
	}
	text := renderMessages(ch, th, msgs, format)
	filename := fmt.Sprintf("%s_%s.md", safeName(ch), strings.ReplaceAll(th, ".", "_"))
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
			"channel":       ch,
			"thread_ts":     th,
			"message_count": len(msgs),
		},
	}, nil
}

func resolveChannelID(name, token string) (string, error) {
	cursor := ""
	for {
		u := "https://slack.com/api/conversations.list?limit=200&types=public_channel,private_channel"
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		body, err := apiGet(u, token)
		if err != nil {
			return "", err
		}
		var data struct {
			OK      bool   `json:"ok"`
			Error   string `json:"error"`
			Channel []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"channels"`
			ResponseMetadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := json.Unmarshal(body, &data); err != nil {
			return "", err
		}
		if !data.OK {
			return "", errors.New(data.Error)
		}
		for _, c := range data.Channel {
			if c.Name == name {
				return c.ID, nil
			}
		}
		if data.ResponseMetadata.NextCursor == "" {
			break
		}
		cursor = data.ResponseMetadata.NextCursor
	}
	return "", fmt.Errorf("channel not found: %s", name)
}

func fetchReplies(channel, thread, token string) ([]Message, error) {
	cursor := ""
	out := make([]Message, 0, 32)
	for {
		u := fmt.Sprintf("https://slack.com/api/conversations.replies?channel=%s&ts=%s&limit=200", url.QueryEscape(channel), url.QueryEscape(thread))
		if cursor != "" {
			u += "&cursor=" + url.QueryEscape(cursor)
		}
		body, err := apiGet(u, token)
		if err != nil {
			return nil, err
		}
		var resp apiResp
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, err
		}
		if !resp.OK {
			return nil, errors.New(resp.Error)
		}
		out = append(out, resp.Messages...)
		if !resp.HasMore || resp.ResponseMetadata.NextCursor == "" {
			break
		}
		cursor = resp.ResponseMetadata.NextCursor
	}
	return out, nil
}

func renderMessages(channel, thread string, msgs []Message, format string) string {
	capturedAt := time.Now().Format(time.RFC3339)
	if format == "text" {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("channel=%s thread_ts=%s captured_at=%s\n\n", channel, thread, capturedAt))
		for _, m := range msgs {
			sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", m.Ts, m.User, strings.TrimSpace(m.Text)))
		}
		return sb.String()
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Slack thread** channel=%s thread_ts=%s captured_at=%s\n\n", channel, thread, capturedAt))
	for _, m := range msgs {
		ts := m.Ts
		if f, err := strconv.ParseFloat(m.Ts, 64); err == nil {
			ts = time.Unix(int64(f), 0).Format(time.RFC3339)
		}
		sb.WriteString(fmt.Sprintf("- `%s` **%s**: %s\n", ts, m.User, strings.TrimSpace(m.Text)))
	}
	return sb.String()
}

func apiGet(url, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	cli := &http.Client{Timeout: 30 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("slack api %d: %s", resp.StatusCode, string(b))
	}
	return io.ReadAll(resp.Body)
}

func safeName(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	return s
}
