package ill_agent

import (
	"context"
	"testing"
)

func TestInferRestartPlanTreatsOrdinaryReviewFeedbackAsResume(t *testing.T) {
	state := &IllustrationSessionState{State: "image_review"}
	plan := InferRestartPlan(context.Background(), "小女孩的头发短一点，背景再亮一点", state, false)
	if plan.ShouldRestart {
		t.Fatalf("ordinary feedback should not restart: %+v", plan)
	}
}

func TestInferRestartPlanRequiresExplicitRestartIntentForStageWords(t *testing.T) {
	state := &IllustrationSessionState{State: "character_review"}
	plan := InferRestartPlan(context.Background(), "角色形象更圆润一点", state, false)
	if plan.ShouldRestart {
		t.Fatalf("stage words alone should not restart: %+v", plan)
	}
}

func TestInferRestartPlanRestartsOnExplicitRestartIntent(t *testing.T) {
	state := &IllustrationSessionState{State: "image_review"}
	plan := InferRestartPlan(context.Background(), "重新生成第2章图片，角色表情更开心", state, false)
	if !plan.ShouldRestart {
		t.Fatalf("explicit restart intent should restart: %+v", plan)
	}
	if plan.Stage != StageImage {
		t.Fatalf("stage = %q, want %q", plan.Stage, StageImage)
	}
	if plan.ChapterIndex != 1 {
		t.Fatalf("chapter index = %d, want 1", plan.ChapterIndex)
	}
}

func TestHasExplicitRestartIntentFromStagePattern(t *testing.T) {
	if !HasExplicitRestartIntent("从角色阶段开始重新处理") {
		t.Fatal("expected explicit restart intent")
	}
	if HasExplicitRestartIntent("从左到右移动一下镜头") {
		t.Fatal("ordinary directional feedback should not be restart intent")
	}
}
