package title

import "testing"

func TestValidTitleEnforcesGenerationContract(t *testing.T) {
	tests := map[string]struct {
		title string
		valid bool
	}{
		"Chinese semantic title": {title: "产品经理面试准备", valid: true},
		"English semantic title": {title: "Product interview preparation", valid: true},
		"mixed language title":   {title: "IELTS 口语练习", valid: true},
		"one word":               {title: "Interview", valid: false},
		"too many words": {
			title: "one two three four five six seven eight nine ten eleven twelve thirteen",
			valid: false,
		},
		"Markdown heading":     {title: "# Interview preparation", valid: false},
		"sentence punctuation": {title: "Interview preparation!", valid: false},
		"emoji":                {title: "Interview preparation 🎯", valid: false},
		"label":                {title: "Title: Interview preparation", valid: false},
		"quotation marks":      {title: "“Interview preparation”", valid: false},
	}
	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			if got := ValidTitle(testCase.title); got != testCase.valid {
				t.Fatalf("ValidTitle(%q) = %t, want %t", testCase.title, got, testCase.valid)
			}
		})
	}
}
