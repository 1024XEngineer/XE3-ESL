package speechfeedback

import "testing"

func TestSpeechFeedbackOralReferenceText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		transcript string
		want       string
	}{
		{
			name:       "because joins complete spoken clauses",
			transcript: "I called the landlord. Because the air conditioner is leaking.",
			want:       "I called the landlord because the air conditioner is leaking.",
		},
		{
			name:       "gate zero 800 millisecond pause transcript",
			transcript: "I called the lender. Because the air conditioner is leaking. And I need someone to repair it tomorrow.",
			want:       "I called the lender because the air conditioner is leaking, and I need someone to repair it tomorrow.",
		},
		{
			name:       "and joins complete spoken clauses",
			transcript: "The air conditioner is leaking. And I need someone to repair it tomorrow.",
			want:       "The air conditioner is leaking, and I need someone to repair it tomorrow.",
		},
		{
			name:       "standalone subordinate fragment remains evidence",
			transcript: "Because the air conditioner is leaking.",
			want:       "Because the air conditioner is leaking.",
		},
		{
			name:       "incomplete continuation is not hidden",
			transcript: "I called the landlord. Because the air conditioner.",
			want:       "I called the landlord. Because the air conditioner.",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := speechFeedbackOralReferenceText(test.transcript); got != test.want {
				t.Fatalf("speechFeedbackOralReferenceText() = %q, want %q", got, test.want)
			}
		})
	}
}
