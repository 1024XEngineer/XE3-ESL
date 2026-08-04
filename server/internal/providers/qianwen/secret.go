package qianwen

// providerSecret keeps the credential behind a closure so default reflection
// of any containing provider value can reveal only a function address, never
// the captured bytes. Its formatting and JSON surfaces always redact.
type providerSecret struct {
	revealValue func() string
}

func newProviderSecret(value string) providerSecret {
	return providerSecret{
		revealValue: func() string {
			return value
		},
	}
}

func (secret providerSecret) reveal() string {
	if secret.revealValue == nil {
		return ""
	}
	return secret.revealValue()
}

func (providerSecret) String() string {
	return "[REDACTED]"
}

func (providerSecret) GoString() string {
	return "qianwen.providerSecret([REDACTED])"
}

func (providerSecret) MarshalJSON() ([]byte, error) {
	return []byte(`"[REDACTED]"`), nil
}
