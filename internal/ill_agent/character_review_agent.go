package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/model"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type CharacterReviewAgent struct {
	AgentName string
	AgentDesc string
}

func NewCharacterReviewAgent(ctx context.Context) adk.Agent {
	return CharacterReviewAgent{
		AgentName: "角色内容审核助手",
		AgentDesc: "审核全局角色设定和角色参考图是否符合故事要求",
	}
}

func (r CharacterReviewAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r CharacterReviewAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r CharacterReviewAgent) Run(ctx context.Context, input *adk.AgentInput, options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		if len(sessionState.Characters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("characters not found in session")})
			return
		}

		sessionState.State = "character_review"
		SaveSessionState(ctx, sessionState)

		infoList := make([]map[string]interface{}, 0, len(sessionState.Characters)+2)
		infoList = append(infoList, map[string]interface{}{
			"text": "已生成全局角色设定和角色参考图：",
		})
		infoList = append(infoList, map[string]interface{}{
			"characterRefs": buildCharacterRefs(sessionState.Characters),
		})
		infoList = append(infoList, map[string]interface{}{
			"text": "如果角色设定和参考图符合要求，请回复ok。否则请描述需要修改的角色外观、服装或关系。",
		})

		gen.Send(adk.StatefulInterrupt(ctx, infoList, sessionState.State))
	}()

	return iter
}

func (r CharacterReviewAgent) Resume(ctx context.Context, info *adk.ResumeInfo, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		if info.ResumeData == nil {
			gen.Send(&adk.AgentEvent{Err: errors.New("character review agent receives nil resume data")})
			return
		}
		feedback, ok := info.ResumeData.(string)
		if !ok {
			gen.Send(&adk.AgentEvent{Err: errors.New("character review agent receives invalid resume data")})
			return
		}

		sessionState := GetSessionState(ctx)
		if strings.ToLower(strings.TrimSpace(feedback)) != "ok" {
			sessionState.NeedToEditCharacters = true
			sessionState.CharacterFeedback = feedback
		} else {
			sessionState.NeedToEditCharacters = false
			sessionState.CharacterFeedback = ""
		}
		SaveSessionState(ctx, sessionState)

		if !sessionState.NeedToEditCharacters {
			gen.Send(&adk.AgentEvent{Action: adk.NewBreakLoopAction(r.AgentName)})
			return
		}
		if strings.TrimSpace(sessionState.CharacterFeedback) == "" {
			gen.Send(&adk.AgentEvent{Err: errors.New("character feedback is empty")})
			return
		}

		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: sessionState.CharacterFeedback,
					},
				},
			},
		})
	}()

	return iter
}

func buildCharacterRefs(characters []model.CharacterProfile) []map[string]interface{} {
	refs := make([]map[string]interface{}, 0, len(characters))
	for _, character := range characters {
		refs = append(refs, map[string]interface{}{
			"id":                 character.ID,
			"name":               character.Name,
			"role":               character.Role,
			"description":        character.Description,
			"visualPrompt":       character.VisualPrompt,
			"aliases":            character.Aliases,
			"chapterIndices":     character.ChapterIndices,
			"referenceImageUrls": character.ReferenceImageURLs,
			"text":               fmt.Sprintf("%s：%s", character.Name, character.Description),
			"imageUrls":          character.ReferenceImageURLs,
		})
	}
	return refs
}
