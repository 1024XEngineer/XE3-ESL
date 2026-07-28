package config

import "testing"

func TestLoadEmbeddingRequiresExplicitProductionConfiguration(t *testing.T) {
	t.Setenv("EMBEDDING_PROVIDER", "qianwen")
	t.Setenv(
		"QIANWEN_EMBEDDING_BASE_URL",
		"https://dashscope.aliyuncs.com/compatible-mode/v1",
	)
	t.Setenv("QIANWEN_EMBEDDING_MODEL", "text-embedding-v4")
	t.Setenv("QIANWEN_EMBEDDING_DIMENSIONS", "1024")
	t.Setenv("QIANWEN_EMBEDDING_TIMEOUT", "45s")
	t.Setenv("DASHSCOPE_API_KEY", "test-secret")

	configuration, err := LoadEmbedding()
	if err != nil {
		t.Fatalf("LoadEmbedding: %v", err)
	}
	if configuration.Provider != EmbeddingProviderQianwen ||
		configuration.Model != "text-embedding-v4" ||
		configuration.Dimensions != 1024 ||
		configuration.APIKey.Reveal() != "test-secret" {
		t.Fatalf("configuration = %#v", configuration)
	}

	t.Setenv("QIANWEN_EMBEDDING_DIMENSIONS", "768")
	if _, err = LoadEmbedding(); err == nil {
		t.Fatal("expected schema dimension mismatch")
	}
}

func TestLoadEmbeddingRejectsMissingOrUnsupportedProvider(t *testing.T) {
	t.Setenv("EMBEDDING_PROVIDER", "")
	if _, err := LoadEmbedding(); err == nil {
		t.Fatal("expected missing provider error")
	}
	t.Setenv("EMBEDDING_PROVIDER", "fake")
	if _, err := LoadEmbedding(); err == nil {
		t.Fatal("expected unsupported provider error")
	}
}
