package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/model"
	"illustration2/internal/volc"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type ImagePromptAgent struct {
	AgentName string
	AgentDesc string
	ModelName string
	ArkClient *volc.ArkClient
}

func NewImagePromptAgent(ctx context.Context) adk.Agent {
	a := ImagePromptAgent{
		AgentName: "ImagePromptAgent",
		AgentDesc: `You are a professional image prompt word generation assistant that generates corresponding text and image prompt words based on user input content.Only output the final English prompt words, without the need for additional information.
User input content: 
%s`,
		ModelName: "ep-20250220181854-c8s82",
		ArkClient: volc.NewArkClientDefault(),
	}
	return a
}

func (r ImagePromptAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r ImagePromptAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r ImagePromptAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		// 调用工具生成每个章节的图片提示词
		imagePrompts := make([]model.ImagePrompt, 0)
		for i, chapter := range sessionState.Story.Chapters {
			prompt := fmt.Sprintf(r.AgentDesc, chapter.Content)
			content, err := r.ArkClient.ChatJSON(ctx, r.ModelName, prompt)
			if err != nil {
				event := &adk.AgentEvent{
					Err: errors.New("image prompt generation failed"),
				}
				gen.Send(event)
				return
			}
			imagePrompts = append(imagePrompts, model.ImagePrompt{
				ChapterIndex: i,
				Prompt:       content,
			})
		}
		fmt.Printf("imagePrompts: %+v\n", imagePrompts)
		sessionState.ImagePrompts = imagePrompts
		sessionState.State = "image_prompt"
		SaveSessionState(ctx, sessionState)

		event := &adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: "Image prompts generated successfully",
					},
				},
			},
		}
		gen.Send(event)
	}()

	return iter
}
