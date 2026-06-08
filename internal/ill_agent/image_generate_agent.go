package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/volc"
	"log"
	"strings"
	"time"

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
		AgentName: "图片生成助手",
		AgentDesc: ``,
		ModelName: "ep-20251124201143-rwjnq",
		ArkClient: volc.NewArkClientWithTimeout(180 * time.Second),
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
		if sessionState.Story == nil || len(sessionState.Story.Chapters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("story is empty, cannot generate first frame image")})
			return
		}
		if sessionState.CurrentImageChapter < 0 || sessionState.CurrentImageChapter >= len(sessionState.Story.Chapters) {
			gen.Send(&adk.AgentEvent{Err: errors.New("current image chapter is out of range")})
			return
		}
		if sessionState.GeneratedImages == nil {
			sessionState.GeneratedImages = make(map[int][]string)
		}
		chapterIndex := sessionState.CurrentImageChapter
		imagePrompt := ""
		for _, prompt := range sessionState.ImagePrompts {
			if prompt.ChapterIndex == chapterIndex {
				imagePrompt = strings.TrimSpace(prompt.Prompt)
				break
			}
		}
		if imagePrompt == "" {
			chapter := sessionState.Story.Chapters[chapterIndex]
			imagePrompt = fmt.Sprintf("%s\n%s", strings.TrimSpace(chapter.Title), strings.TrimSpace(chapter.Content))
		}

		referenceImages := ReferenceImagesForCharacters(CharactersForChapter(sessionState.Characters, chapterIndex))
		imageInputs := append([]string{}, referenceImages...)
		if sessionState.ImageFeedback != "" && len(sessionState.GeneratedImages[chapterIndex]) > 0 {
			imageInputs = append(imageInputs, sessionState.GeneratedImages[chapterIndex]...)
			imagePrompt = fmt.Sprintf("%s\nRevision feedback: %s", imagePrompt, strings.TrimSpace(sessionState.ImageFeedback))
		}

		generateImagesReq := volc.ImageGenParams{
			Model:                     r.ModelName,
			Prompt:                    imagePrompt,
			Size:                      "2304x1296",
			SequentialImageGeneration: "disabled",
			MaxImages:                 1,
			ImageInputs:               imageInputs,
		}
		urls, err := r.ArkClient.GenerateImages(ctx, generateImagesReq)
		if err != nil {
			log.Fatal(fmt.Errorf("image generation failed: %+v", err))
			event := &adk.AgentEvent{
				Err: errors.New("image generation failed"),
			}
			gen.Send(event)
			return
		}
		sessionState.GeneratedImages[chapterIndex] = urls
		log.Printf("generatedImages: %+v\n", sessionState.GeneratedImages)
		sessionState.State = "image_generate"
		SaveSessionState(ctx, sessionState)

		event := &adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: fmt.Sprintf("Chapter %d first frame image generated", chapterIndex+1),
					},
				},
			},
		}
		gen.Send(event)
	}()

	return iter
}
