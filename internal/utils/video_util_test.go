package utils

import (
	"strings"
	"testing"
)

func TestBuildFadeFilterUsesTransitionDuration(t *testing.T) {
	filter := buildFadeFilter(3, []float64{9.5, 9.4, 8.6}, 1.0)

	if count := strings.Count(filter, "duration=1.000"); count != 2 {
		t.Fatalf("xfade duration count = %d, want 2 in %q", count, filter)
	}
	if count := strings.Count(filter, "acrossfade=d=1.000"); count != 2 {
		t.Fatalf("acrossfade duration count = %d, want 2 in %q", count, filter)
	}
	if !strings.Contains(filter, "offset=8.500") {
		t.Fatalf("filter does not contain first offset: %q", filter)
	}
}
