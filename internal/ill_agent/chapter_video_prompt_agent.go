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

type ChapterVideoPromptAgent struct {
	AgentName string
	AgentDesc string
	ModelName string
	ArkClient *volc.ArkClient
}

func NewChapterVideoPromptAgent(ctx context.Context) adk.Agent {
	a := ChapterVideoPromptAgent{
		AgentName: "章节视频提示词助手",
		AgentDesc: `You are a professional video prompt engineer. Convert the user-provided story (theme + chapters) and any visual feedback into ONE high-quality English video generation prompt.

Requirements:
- Output English prompt only. No extra text.
- Keep it concise but specific (visual style, characters, environment, mood, motion, camera language, continuity).
- Ensure temporal continuity across scenes; avoid abrupt style changes.
- Avoid on-screen text, subtitles, logos, watermarks.
- If duration is needed, assume 10–15 seconds, 16:9, 24fps, cinematic lighting, smooth motion.

Input:
Theme: %s
Chapters:
%s
`,
		ModelName: "ep-20250220181854-c8s82",
		ArkClient: volc.NewArkClientDefault(),
	}
	return a
}

func (r ChapterVideoPromptAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r ChapterVideoPromptAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r ChapterVideoPromptAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		if sessionState.Story == nil || len(sessionState.Story.Chapters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("story is empty, cannot generate chapter video prompts")})
			return
		}

		chapterVideoPrompts := make([]model.VideoPrompt, 0, len(sessionState.Story.Chapters))
		for i, chapter := range sessionState.Story.Chapters {
			sceneDesc := fmt.Sprintf("Scene %d: %s. %s", i+1, strings.TrimSpace(chapter.Title), strings.TrimSpace(chapter.Content))
			prompt := fmt.Sprintf(
				r.AgentDesc,
				strings.TrimSpace(sessionState.Story.Theme),
				sceneDesc,
			)

			content, err := r.ArkClient.ChatJSON(ctx, r.ModelName, prompt)
			if err != nil {
				gen.Send(&adk.AgentEvent{Err: errors.New("chapter video prompt generation failed")})
				return
			}
			chapterVideoPrompts = append(chapterVideoPrompts, model.VideoPrompt{
				ChapterIndex: i,
				Prompt:       content,
			})
		}

		sessionState.ChapterVideoPrompts = chapterVideoPrompts
		sessionState.State = "chapter_video_prompt"
		SaveSessionState(ctx, sessionState)

		log.Printf("chapterVideoPrompts: %+v\n", chapterVideoPrompts)

		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: "Chapter video prompts generated successfully",
					},
				},
			},
		})
	}()

	return iter
}
