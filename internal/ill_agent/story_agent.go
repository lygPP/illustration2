package ill_agent

import (
	"context"
	"fmt"
	"log"

	"os"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/adk"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

func NewStoryAgent(ctx context.Context) adk.Agent {
	apiKey := os.Getenv("ARK_API_KEY")
	chatModel, err := ark.NewChatModel(context.Background(), &ark.ChatModelConfig{
		APIKey:  apiKey,
		Model:   "ep-20250220181854-c8s82",
		BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
		Thinking: &arkModel.Thinking{
			Type: arkModel.ThinkingTypeDisabled,
		},
	})

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "StoryGenerateAgent",
		Description: "An agent that can generate children's illustration story",
		Instruction: `You are an expert writer that can generate children's illustration story. 
If feedback is received for the previous version of your story, you need to modify the story according to the feedback.
Your response should be a JSON format string, JSON format: {"chapters": [{"title": "xx", "content": "xxxx"}]}`,
		Model:     chatModel,
		OutputKey: "story_content_to_review",
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create chatmodel: %w", err))
	}

	la, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:        "Story MultiAgent",
		Description: "An agent that can generate children's illustration story",
		SubAgents: []adk.Agent{a,
			&StoryReviewAgent{AgentName: "StoryReviewerAgent", AgentDesc: "An agent that can review story"}},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create loopagent: %w", err))
	}

	return la
}
