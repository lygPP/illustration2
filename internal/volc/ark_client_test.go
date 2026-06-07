package volc

import (
	"context"
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
