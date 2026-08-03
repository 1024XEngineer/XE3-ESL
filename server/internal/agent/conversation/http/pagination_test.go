package conversationhttp

import "testing"

func TestDecodeAgentPageQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         string
		defaultSize int
		wantSize    int
		wantCursor  string
		wantOK      bool
	}{
		{
			name:        "omitted",
			defaultSize: defaultThreadPageSize,
			wantSize:    defaultThreadPageSize,
			wantOK:      true,
		},
		{
			name:        "maximum and cursor",
			raw:         "page_size=100&cursor=opaque",
			defaultSize: defaultMessagePageSize,
			wantSize:    100,
			wantCursor:  "opaque",
			wantOK:      true,
		},
		{
			name:        "zero",
			raw:         "page_size=0",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "above maximum",
			raw:         "page_size=101",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "non decimal",
			raw:         "page_size=+20",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "duplicate size",
			raw:         "page_size=20&page_size=21",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "empty cursor",
			raw:         "cursor=",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "unknown query",
			raw:         "offset=20",
			defaultSize: defaultThreadPageSize,
		},
		{
			name:        "invalid encoding",
			raw:         "cursor=%zz",
			defaultSize: defaultThreadPageSize,
		},
	}
	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			size, cursor, ok := decodePageQuery(
				testCase.raw,
				testCase.defaultSize,
			)
			if size != testCase.wantSize ||
				cursor != testCase.wantCursor ||
				ok != testCase.wantOK {
				t.Fatalf(
					"decodePageQuery(%q) = (%d, %q, %t), want (%d, %q, %t)",
					testCase.raw,
					size,
					cursor,
					ok,
					testCase.wantSize,
					testCase.wantCursor,
					testCase.wantOK,
				)
			}
		})
	}
}
