package conversation

import "testing"

func TestDeriveThreadTitle(t *testing.T) {
	t.Parallel()
	if got := DeriveThreadTitle("  Prepare\nmy\tinterview  "); got != "Prepare my interview" {
		t.Fatalf("title = %q", got)
	}
	if got := []rune(DeriveThreadTitle("一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三")); len(got) != MaxThreadTitleRunes {
		t.Fatalf("title rune count = %d", len(got))
	}
}
