package feedback_loop_example

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/adk"

	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

func NewWriterAgent() adk.Agent {
	ctx := context.Background()
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
		Name:        "WriterAgent",
		Description: "An agent that can write poems",
		Instruction: `You are an expert writer that can write poems. 
If feedback is received for the previous version of your poem, you need to modify the poem according to the feedback.
Your response should ALWAYS contain ONLY the poem, and nothing else.`,
		// Model:     model.NewChatModel(),
		Model:     chatModel,
		OutputKey: "content_to_review",
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create chatmodel: %w", err))
	}

	la, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:        "Writer MultiAgent",
		Description: "An agent that can write poems",
		SubAgents: []adk.Agent{a,
			&ReviewAgent{AgentName: "ReviewerAgent", AgentDesc: "An agent that can review poems"}},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create loopagent: %w", err))
	}

	return la
}
