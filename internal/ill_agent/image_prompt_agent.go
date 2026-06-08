package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/model"
	"illustration2/internal/volc"
	"log"
	"strings"

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
		AgentName: "图片提示词助手",
		AgentDesc: `You are a professional children's book illustration prompt engineer. Create ONE English image prompt for the first frame of this chapter.

Requirements:
- Output the final English prompt only. No extra text.
- The image is a single polished 16:9 first-frame illustration, not a sequence or storyboard.
- Base the composition on the chapter content and make it suitable as the first frame for a later video.
- Keep character appearance consistent with the character bible and reference images.
- Include visual style, characters, environment, mood, composition, lighting, and camera framing.
- Avoid on-screen text, subtitles, logos, watermarks.

Theme: %s
Chapter:
%s
Characters in this chapter:
%s`,
		ModelName: "ep-20260608120832-fq5kh",
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
		imagePrompts := make([]model.ImagePrompt, 0)
		for i, chapter := range sessionState.Story.Chapters {
			sceneDesc := fmt.Sprintf("Chapter %d: %s. %s", i+1, strings.TrimSpace(chapter.Title), strings.TrimSpace(chapter.Content))
			chapterCharacters := CharactersForChapter(sessionState.Characters, i)
			characterDesc := FormatCharactersForPrompt(chapterCharacters)
			prompt := fmt.Sprintf(
				r.AgentDesc,
				strings.TrimSpace(sessionState.Story.Theme),
				sceneDesc,
				characterDesc,
			)
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
		log.Printf("imagePrompts: %+v\n", imagePrompts)
		sessionState.ImagePrompts = imagePrompts
		sessionState.CurrentImageChapter = 0
		sessionState.ImageFeedback = ""
		sessionState.NeedToEditImages = false
		if sessionState.GeneratedImages == nil {
			sessionState.GeneratedImages = make(map[int][]string)
		}
		if sessionState.ConfirmedImages == nil {
			sessionState.ConfirmedImages = make(map[int][]string)
		}
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
