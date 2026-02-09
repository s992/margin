package slackcap

import "testing"

func TestParseThreadInputURL(t *testing.T) {
	channel, thread, err := ParseThreadInput("", "https://example.slack.com/archives/C12345678/p1700000000123456")
	if err != nil {
		t.Fatal(err)
	}
	if channel != "C12345678" {
		t.Fatalf("channel=%s", channel)
	}
	if thread == "" {
		t.Fatal("thread empty")
	}
}
