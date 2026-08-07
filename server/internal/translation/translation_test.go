package translation

import "testing"

func TestProviderErrorKindsExposeRetryability(t *testing.T) {
	t.Parallel()
	tests := map[ProviderErrorKind]bool{
		ProviderErrorInvalidRequest:  false,
		ProviderErrorConfiguration:   false,
		ProviderErrorAuthentication:  false,
		ProviderErrorAuthorization:   false,
		ProviderErrorQuotaExhausted:  false,
		ProviderErrorRateLimited:     true,
		ProviderErrorTimeout:         true,
		ProviderErrorUnavailable:     true,
		ProviderErrorInvalidResponse: true,
		ProviderErrorCancelled:       true,
	}
	for kind, want := range tests {
		if got := kind.Retryable(); got != want {
			t.Errorf("%s Retryable = %t, want %t", kind, got, want)
		}
	}
}
