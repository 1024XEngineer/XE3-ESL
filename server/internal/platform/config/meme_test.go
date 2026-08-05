package config

import "testing"

func TestLoadMemeUsesAlwaysOnSingleImageDefaults(t *testing.T) {
	for _, name := range []string{
		"AGENT_MEME_ENABLED", "AGENT_MEME_SEND_PROBABILITY",
		"AGENT_MEME_MAX_PER_MESSAGE", "AGENT_MEME_AVOID_RECENT_COUNT",
		"AGENT_MEME_CLASSIFICATION_TIMEOUT", "AGENT_MEME_ASSET_ROOT",
		"AGENT_MEME_DEFAULT_CATEGORY", "AGENT_MEME_PACK_ID",
		"AGENT_MEME_PACK_VERSION",
	} {
		t.Setenv(name, "")
	}
	configuration, err := LoadMeme()
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Runtime.Enabled || configuration.Runtime.SendProbability != 1 ||
		configuration.Runtime.MaxPerMessage != 1 || configuration.AssetRoot != "assets/memes" {
		t.Fatalf("LoadMeme defaults = %#v", configuration)
	}
}

func TestLoadMemeRejectsInvalidProbability(t *testing.T) {
	t.Setenv("AGENT_MEME_SEND_PROBABILITY", "1.1")
	if _, err := LoadMeme(); err == nil {
		t.Fatal("invalid Meme probability must fail startup configuration")
	}
}
