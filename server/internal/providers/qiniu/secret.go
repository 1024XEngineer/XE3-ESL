package qiniu

import "encoding/json"

type providerSecret struct {
	value string
}

func newProviderSecret(value string) providerSecret {
	return providerSecret{value: value}
}

func (secret providerSecret) reveal() string {
	return secret.value
}

func (providerSecret) String() string {
	return "[REDACTED]"
}

func (providerSecret) GoString() string {
	return "qiniu.providerSecret([REDACTED])"
}

func (providerSecret) MarshalJSON() ([]byte, error) {
	return json.Marshal("[REDACTED]")
}
