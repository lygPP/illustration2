package indextts

import (
	"testing"
	"time"
)

func TestNewClientFromEnvUsesLongDefaultTimeout(t *testing.T) {
	t.Setenv("INDEX_TTS_TIMEOUT_SECONDS", "")

	client := NewClientFromEnv()

	if client.HTTPClient.Timeout != time.Duration(defaultTimeoutSeconds)*time.Second {
		t.Fatalf("timeout = %s, want %s", client.HTTPClient.Timeout, time.Duration(defaultTimeoutSeconds)*time.Second)
	}
}

func TestNewClientFromEnvAllowsTimeoutOverride(t *testing.T) {
	t.Setenv("INDEX_TTS_TIMEOUT_SECONDS", "42")

	client := NewClientFromEnv()

	if client.HTTPClient.Timeout != 42*time.Second {
		t.Fatalf("timeout = %s, want 42s", client.HTTPClient.Timeout)
	}
}
