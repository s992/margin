package slackcap

import "testing"

func TestParseTranscriptContinuationSameTimestamp(t *testing.T) {
	in := `sean  [10:48 AM]
 hello world
 Sarine  [10:49 AM]
 great
 [10:49 AM]follow up`
	msgs := ParseTranscript(in)
	if len(msgs) != 2 {
		t.Fatalf("len=%d", len(msgs))
	}
	if msgs[1].User != "Sarine" || msgs[1].Ts != "10:49 AM" {
		t.Fatalf("unexpected message header: %+v", msgs[1])
	}
	if msgs[1].Text != "great\nfollow up" {
		t.Fatalf("text=%q", msgs[1].Text)
	}
}

func TestParseTranscriptTimestampPrefixStartsNewMessage(t *testing.T) {
	in := `Sarine  [10:49 AM]
 great
 [11:55 AM]follow up`
	msgs := ParseTranscript(in)
	if len(msgs) != 2 {
		t.Fatalf("len=%d", len(msgs))
	}
	if msgs[1].User != "Sarine" || msgs[1].Ts != "11:55 AM" {
		t.Fatalf("unexpected second message: %+v", msgs[1])
	}
	if msgs[1].Text != "follow up" {
		t.Fatalf("text=%q", msgs[1].Text)
	}
}
