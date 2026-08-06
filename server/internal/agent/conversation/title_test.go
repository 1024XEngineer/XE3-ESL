package conversation

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDeriveThreadTitle(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		input string
		want  string
	}{
		"empty": {input: " \n\t ", want: ""},
		"collapse whitespace": {
			input: "  Help me\nprepare\tEnglish  ",
			want:  "Help me prepare English",
		},
		"short Unicode": {input: "我想练习英文自我介绍", want: "我想练习英文自我介绍"},
		"truncate Unicode": {
			input: strings.Repeat("面", ThreadTitleContentLimit+1),
			want:  strings.Repeat("面", ThreadTitleContentLimit) + "…",
		},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := DeriveThreadTitle(testCase.input)
			if got != testCase.want {
				t.Fatalf("DeriveThreadTitle(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
			if utf8.RuneCountInString(got) > ThreadTitleContentLimit+1 {
				t.Fatalf("title length = %d", utf8.RuneCountInString(got))
			}
		})
	}
}
