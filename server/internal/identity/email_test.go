package identity

import "testing"

func TestNormalizeEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercases and trims ASCII", "\t Learner@XN--BCHER-KVA.example\r", "learner@xn--bcher-kva.example"},
		{"preserves plus addressing", "First.Last+tag@Example.COM", "first.last+tag@example.com"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeEmail(test.input)
			if err != nil || got != test.want {
				t.Fatalf("NormalizeEmail() = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestNormalizeEmailRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"learner@example",
		"learner@example..com",
		"learner\u00a0@example.com",
		"ü@example.com",
		".learner@example.com",
		"learner@-example.com",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := NormalizeEmail(input); err == nil {
				t.Fatalf("expected %q to be rejected", input)
			}
		})
	}
}
