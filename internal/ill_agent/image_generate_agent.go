package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/volc"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type ImageGenerateAgent struct {
	AgentName string
	AgentDesc string
	ModelName string
	ArkClient *volc.ArkClient
}

func NewImageGenerateAgent(ctx context.Context) adk.Agent {
	a := ImageGenerateAgent{
		AgentName: "ImageGenerateAgent",
		AgentDesc: ``,
		ModelName: "ep-20251124201143-rwjnq",
		ArkClient: volc.NewArkClientDefault(),
	}
	return a
}

func (r ImageGenerateAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r ImageGenerateAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r ImageGenerateAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		// 调用工具生成每个章节的图片提示词
		generatedImages := make(map[int][]string)
		for _, prompt := range sessionState.ImagePrompts {
			generateImagesReq := volc.ImageGenParams{
				Model:                     r.ModelName,
				Prompt:                    fmt.Sprintf("%s\n%s", prompt.Prompt, "Additional generation requirement: Each set of images should include three consecutive images related to the scene."),
				Size:                      "2304x1728",
				SequentialImageGeneration: "auto",
				MaxImages:                 3,
			}
			if sessionState.ImageFeedback != "" {
				generateImagesReq.Prompt = fmt.Sprintf("%s\n%s", generateImagesReq.Prompt, sessionState.ImageFeedback)
				if len(sessionState.GeneratedImages[prompt.ChapterIndex]) > 0 {
					generateImagesReq.ImageInputs = sessionState.GeneratedImages[prompt.ChapterIndex]
				}
			}
			urls, err := r.ArkClient.GenerateImages(ctx, generateImagesReq)
			if err != nil {
				event := &adk.AgentEvent{
					Err: errors.New("image generation failed"),
				}
				gen.Send(event)
				return
			}
			generatedImages[prompt.ChapterIndex] = urls
		}
		fmt.Printf("generatedImages: %+v\n", generatedImages)
		sessionState.GeneratedImages = generatedImages
		sessionState.State = "image_generate"
		SaveSessionState(ctx, sessionState)

		event := &adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: "Image generation completed",
					},
				},
			},
		}
		gen.Send(event)
	}()

	return iter
}
