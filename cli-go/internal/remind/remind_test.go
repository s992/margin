package remind

import "testing"

func TestParseWhen(t *testing.T) {
	tm, err := parseWhen("2026-01-02")
	if err != nil {
		t.Fatal(err)
	}
	if tm.Hour() != 9 || tm.Minute() != 0 {
		t.Fatalf("unexpected default time: %v", tm)
	}
	_, err = parseWhen("2026-01-02 14:05")
	if err != nil {
		t.Fatal(err)
	}
}
