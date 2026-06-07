package ill_agent

import (
	"context"
	"fmt"
	"log"

	"github.com/cloudwego/eino/adk"
)

func NewCharacterAgent(ctx context.Context) adk.Agent {
	characterLoopAgent, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:        "角色生成&审核agent",
		Description: "根据完整故事生成全局角色设定和角色参考图，并根据反馈重新生成",
		SubAgents: []adk.Agent{
			NewCharacterGenerateAgent(ctx),
			NewCharacterReviewAgent(ctx),
		},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create characterLoopAgent: %w", err))
	}
	return characterLoopAgent
}

func NewChapterVideoAgent(ctx context.Context) adk.Agent {
	la, err := adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
		Name:        "章节视频小助手",
		Description: "基于全局角色参考图生成章节视频",
		SubAgents: []adk.Agent{
			NewChapterVideoPromptAgent(ctx),
			NewChapterVideoGenerateAgent(ctx),
		},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create chapter video agent: %w", err))
	}
	return la
}
