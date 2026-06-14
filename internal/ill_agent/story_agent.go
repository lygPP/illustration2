package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/usage"
	"illustration2/internal/volc"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const storyModelName = "ep-20260608120832-fq5kh"

const storyGenerateInstruction = `You are an expert writer that can generate children's illustration story.
If feedback is received for the previous version of your story, you need to modify the story according to the feedback.
Each chapter's body should be an appropriate, brief length: concise, visually actionable, and easy to narrate. Avoid long paragraphs, side explanations, and multiple major scene changes inside one chapter.
Your response should contain multiple chapters, and must output strictly in the example format, and only contain the story content,不同章节间用##隔开，同章节的标题和内容用#隔开, eg:
第1章: 一个小苹果#一个小苹果，站在树的枝上，看起来很神秘。##第2章: 苹果的秘密#这个小苹果，它的颜色是黄色的，它的形状是一个圆。

Theme:
%s

Feedback:
%s`

type StoryGenerateAgent struct {
	AgentName string
	AgentDesc string
	ModelName string
	ArkClient *volc.ArkClient
}

func NewStoryAgent(ctx context.Context) adk.Agent {
	storyGenerateAgent := &StoryGenerateAgent{
		AgentName: "故事生成助手",
		AgentDesc: storyGenerateInstruction,
		ModelName: storyModelName,
		ArkClient: volc.NewArkClientWithTimeout(60 * time.Second),
	}

	la, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:        "Story MultiAgent",
		Description: "An agent that can generate children's illustration story",
		SubAgents: []adk.Agent{
			storyGenerateAgent,
			&StoryReviewAgent{AgentName: "故事内容审核助手", AgentDesc: "An agent that can review story"},
		},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create loopagent: %w", err))
	}

	return la
}

func (r *StoryGenerateAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r *StoryGenerateAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r *StoryGenerateAgent) Run(ctx context.Context, input *adk.AgentInput, options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		if ShouldSkipStage(sessionState, StageStory) {
			gen.Send(StageSkippedEvent(r.AgentName))
			return
		}
		theme := strings.TrimSpace(sessionState.Story.Theme)
		if theme == "" {
			theme = latestUserMessage(input)
			sessionState.Story.Theme = theme
		}
		if theme == "" {
			gen.Send(&adk.AgentEvent{Err: errors.New("story theme is empty")})
			return
		}

		var content string
		var err error
		if r.ArkClient != nil && r.ArkClient.Mock {
			content = mockStoryContent(theme)
			_ = usage.Record(ctx, r.ModelName, 0, 0)
		} else {
			prompt := fmt.Sprintf(r.AgentDesc, theme, strings.TrimSpace(sessionState.StoryFeedback))
			content, _, err = r.ArkClient.ChatJSONWithUsage(ctx, r.ModelName, prompt)
			if err != nil {
				gen.Send(&adk.AgentEvent{Err: fmt.Errorf("story generation failed: %w", err)})
				return
			}
		}

		adk.AddSessionValue(ctx, "story_content_to_review", content)
		SaveSessionState(ctx, sessionState)

		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: content,
					},
				},
			},
		})
	}()

	return iter
}

func latestUserMessage(input *adk.AgentInput) string {
	if input == nil {
		return ""
	}
	for i := len(input.Messages) - 1; i >= 0; i-- {
		if input.Messages[i] != nil && strings.TrimSpace(input.Messages[i].Content) != "" {
			return strings.TrimSpace(input.Messages[i].Content)
		}
	}
	return ""
}

func mockStoryContent(theme string) string {
	theme = strings.TrimSpace(theme)
	if theme == "" {
		theme = "奇妙冒险"
	}
	return fmt.Sprintf("第1章: %s的早晨#小伙伴们在明亮的窗边发现一张闪光地图，决定一起出发寻找答案。##第2章: 森林里的线索#他们跟着地图走进柔软的森林，看见会发光的花朵指向前方。##第3章: 温暖的发现#大家在山坡上找到一颗星星种子，明白勇敢和合作就是最好的礼物。", theme)
}
