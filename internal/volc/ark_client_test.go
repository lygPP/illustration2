package volc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMockVoiceCloneAndPreview(t *testing.T) {
	client := &ArkClient{Mock: true}
	result, err := client.CloneVoice(context.Background(), VoiceCloneParams{Name: "旁白", SampleAudioURL: "/uploads/voices/u/sample.wav"})
	if err != nil {
		t.Fatalf("CloneVoice() error = %v", err)
	}
	if result.VoiceType == "" || result.PreviewAudioURL == "" {
		t.Fatalf("CloneVoice() result = %+v", result)
	}
	preview, err := client.GenerateVoicePreview(context.Background(), VoicePreviewParams{Text: "试听", VoiceType: result.VoiceType})
	if err != nil {
		t.Fatalf("GenerateVoicePreview() error = %v", err)
	}
	if preview == "" {
		t.Fatal("GenerateVoicePreview() returned empty preview")
	}
}

func TestChatJSONWithUsageParsesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"content": "ok"}}],
			"usage": {"prompt_tokens": 7, "completion_tokens": 3, "total_tokens": 10}
		}`))
	}))
	defer server.Close()

	client := &ArkClient{BaseURL: server.URL, APIKey: "test", HTTPClient: server.Client()}
	content, usage, err := client.ChatJSONWithUsage(context.Background(), "chat-model", "hello")
	if err != nil {
		t.Fatalf("ChatJSONWithUsage() error = %v", err)
	}
	if content != "ok" {
		t.Fatalf("content = %q", content)
	}
	if usage.PromptTokens != 7 || usage.CompletionTokens != 3 || usage.TotalTokens != 10 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestChatJSONWithUsageMissingUsageKeepsTokensZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "ok"}}]}`))
	}))
	defer server.Close()

	client := &ArkClient{BaseURL: server.URL, APIKey: "test", HTTPClient: server.Client()}
	_, usage, err := client.ChatJSONWithUsage(context.Background(), "chat-model", "hello")
	if err != nil {
		t.Fatalf("ChatJSONWithUsage() error = %v", err)
	}
	if usage.PromptTokens != 0 || usage.CompletionTokens != 0 || usage.TotalTokens != 0 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestChatJSONWithUsageEmptyContentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": ""}}]}`))
	}))
	defer server.Close()

	client := &ArkClient{BaseURL: server.URL, APIKey: "test", HTTPClient: server.Client()}
	_, _, err := client.ChatJSONWithUsage(context.Background(), "chat-model", "hello")
	if err == nil || !strings.Contains(err.Error(), "empty chat content") {
		t.Fatalf("expected empty content error, got %v", err)
	}
}
