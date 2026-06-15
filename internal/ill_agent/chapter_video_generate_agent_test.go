package ill_agent

import (
	"context"
	"testing"
)

func TestNarrationConcurrencyLimitDefaultsToTwo(t *testing.T) {
	t.Setenv("AGENT_NARRATION_CONCURRENCY", "")

	if got := narrationConcurrencyLimit(); got != 2 {
		t.Fatalf("narrationConcurrencyLimit() = %d, want 2", got)
	}
}

func TestFirstNonEmptyString(t *testing.T) {
	if got := firstNonEmptyString("", "  ", " /resource/chapter.mp4 "); got != "/resource/chapter.mp4" {
		t.Fatalf("firstNonEmptyString() = %q, want /resource/chapter.mp4", got)
	}
}

func TestNarrationConcurrencyLimitFallsBackForInvalidLowValues(t *testing.T) {
	t.Setenv("AGENT_NARRATION_CONCURRENCY", "0")

	if got := narrationConcurrencyLimit(); got != 2 {
		t.Fatalf("narrationConcurrencyLimit() = %d, want 2", got)
	}
}

func TestNarrationConcurrencyLimitAllowsOverride(t *testing.T) {
	t.Setenv("AGENT_NARRATION_CONCURRENCY", "2")

	if got := narrationConcurrencyLimit(); got != 2 {
		t.Fatalf("narrationConcurrencyLimit() = %d, want 2", got)
	}
}

func TestAcquireNarrationSlotReturnsContextErrorWhenCanceled(t *testing.T) {
	slots := make(chan struct{}, 1)
	if err := acquireNarrationSlot(context.Background(), slots); err != nil {
		t.Fatalf("acquireNarrationSlot() unexpected error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := acquireNarrationSlot(ctx, slots); err != context.Canceled {
		t.Fatalf("acquireNarrationSlot() error = %v, want %v", err, context.Canceled)
	}
}
