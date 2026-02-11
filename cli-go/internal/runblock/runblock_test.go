package runblock

import "testing"

func TestParseBlocksAndPick(t *testing.T) {
	in := "before\n```python\nprint('x')\n```\nafter\n"
	blocks := ParseBlocks(in)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Language != "python" {
		t.Fatalf("unexpected language: %s", blocks[0].Language)
	}
	picked := PickBlock(blocks, 10)
	if picked == nil {
		t.Fatal("expected block")
	}
}

func TestParseBlocksCRLF(t *testing.T) {
	in := "before\r\n```sh\r\necho hi\r\n```\r\nafter\r\n"
	blocks := ParseBlocks(in)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Language != "sh" {
		t.Fatalf("unexpected language: %s", blocks[0].Language)
	}
}

func TestParseBlocksTildeFence(t *testing.T) {
	in := "~~~python\nprint('x')\n~~~\n"
	blocks := ParseBlocks(in)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].Language != "python" {
		t.Fatalf("unexpected language: %s", blocks[0].Language)
	}
}
