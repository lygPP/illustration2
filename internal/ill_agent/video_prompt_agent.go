package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/volc"
	"log"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type VideoPromptAgent struct {
	AgentName string
	AgentDesc string
	ModelName string
	ArkClient *volc.ArkClient
}

func NewVideoPromptAgent(ctx context.Context) adk.Agent {
	a := VideoPromptAgent{
		AgentName: "视频提示词助手",
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

func (r VideoPromptAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r VideoPromptAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r VideoPromptAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		if sessionState.Story == nil {
			event := &adk.AgentEvent{
				Err: errors.New("story is empty, cannot generate video prompt"),
			}
			gen.Send(event)
			return
		}

		chaptersText := make([]string, 0, len(sessionState.Story.Chapters))
		for i, c := range sessionState.Story.Chapters {
			chaptersText = append(chaptersText, fmt.Sprintf("Scene %d: %s. %s", i+1, strings.TrimSpace(c.Title), strings.TrimSpace(c.Content)))
		}

		prompt := fmt.Sprintf(
			r.AgentDesc,
			strings.TrimSpace(sessionState.Story.Theme),
			strings.Join(chaptersText, "\n"),
		)

		content, err := r.ArkClient.ChatJSON(ctx, r.ModelName, prompt)
		if err != nil {
			event := &adk.AgentEvent{
				Err: errors.New("video prompt generation failed"),
			}
			gen.Send(event)
			return
		}

		sessionState.VideoPrompt = content
		sessionState.State = "video_prompt"
		SaveSessionState(ctx, sessionState)

		log.Printf("videoPrompt: %s\n", content)

		event := &adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: "Video prompt generated successfully",
					},
				},
			},
		}
		gen.Send(event)
	}()

	return iter
}
