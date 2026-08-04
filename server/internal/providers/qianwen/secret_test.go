package qianwen

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestProviderSecretRedactsFormattingAndJSON(t *testing.T) {
	t.Parallel()

	const sensitive = "secret-must-not-appear"
	secret := newProviderSecret(sensitive)
	if secret.reveal() != sensitive {
		t.Fatal("provider secret did not reveal to the request boundary")
	}
	for _, formatted := range []string{
		fmt.Sprintf("%v", secret),
		fmt.Sprintf("%+v", secret),
		fmt.Sprintf("%#v", secret),
		fmt.Sprintf("%v", &secret),
		fmt.Sprintf("%+v", &secret),
		fmt.Sprintf("%#v", &secret),
	} {
		if strings.Contains(formatted, sensitive) {
			t.Fatalf("provider secret formatting leaked: %q", formatted)
		}
	}
	encoded, err := json.Marshal(struct {
		APIKey providerSecret `json:"api_key"`
	}{APIKey: secret})
	if err != nil {
		t.Fatalf("marshal provider secret: %v", err)
	}
	if string(encoded) != `{"api_key":"[REDACTED]"}` {
		t.Fatalf("provider secret JSON = %s", encoded)
	}

	var zero providerSecret
	if zero.reveal() != "" {
		t.Fatal("zero provider secret must reveal an empty value")
	}
	if strings.Contains(fmt.Sprintf("%#v", zero), sensitive) {
		t.Fatal("zero provider secret formatting leaked")
	}
}
