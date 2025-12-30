package ill_agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"illustration2/internal/model"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type ImageReviewAgent struct {
	AgentName string
	AgentDesc string
}

func (r ImageReviewAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r ImageReviewAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r ImageReviewAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		contentToReview, ok := adk.GetSessionValue(ctx, "story_content_to_review")
		if !ok {
			event := &adk.AgentEvent{
				Err: errors.New("story_content_to_review not found in session"),
			}
			gen.Send(event)
			return
		}

		contentToReviewMap := make(map[string][]model.StoryChapter)
		if err := json.Unmarshal([]byte(contentToReview.(string)), &contentToReviewMap); err != nil {
			event := &adk.AgentEvent{
				Err: fmt.Errorf("failed to unmarshal content_to_review: %w", err),
			}
			gen.Send(event)
			return
		}
		chapters := contentToReviewMap["chapters"]
		sessionState := GetSessionState(ctx)
		sessionState.Story.Chapters = chapters
		sessionState.State = "story_review"
		SaveSessionState(ctx, sessionState)

		info := fmt.Sprintf("Story content to review: \n`\n%s\n`. \n\nIf you think the content is good as it is, please reply with \"No need to edit\". \nOtherwise, please provide your feedback.", sessionState.Story.Chapters)
		event := adk.StatefulInterrupt(ctx, info, sessionState.State)
		gen.Send(event)
	}()

	return iter
}

func (r ImageReviewAgent) Resume(ctx context.Context, info *adk.ResumeInfo,
	opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()
		if !info.IsResumeTarget { // not explicitly resumed, interrupt with the same review content again
			sessionState := GetSessionState(ctx)
			info := fmt.Sprintf("Story content to review: \n`\n%s\n`. \n\nIf you think the content is good as it is, please reply with \"No need to edit\". \nOtherwise, please provide your feedback.", sessionState.Story.Chapters)
			event := adk.StatefulInterrupt(ctx, info, sessionState.State)
			gen.Send(event)
			return
		}

		if info.ResumeData == nil {
			event := &adk.AgentEvent{
				Err: errors.New("review agent receives nil resume data"),
			}
			gen.Send(event)
			return
		}

		feedback, ok := info.ResumeData.(string)
		if !ok {
			event := &adk.AgentEvent{
				Err: errors.New("review agent receives invalid resume data"),
			}
			gen.Send(event)
			return
		}

		sessionState := GetSessionState(ctx)
		if strings.ToLower(feedback) == "no need to edit" {
			sessionState.NeedToEditStory = false
		} else {
			sessionState.NeedToEditStory = true
			sessionState.StoryFeedback = feedback
		}
		SaveSessionState(ctx, sessionState)

		if !sessionState.NeedToEditStory {
			event := &adk.AgentEvent{
				Action: adk.NewExitAction(),
			}
			gen.Send(event)
			return
		}

		if sessionState.StoryFeedback == "" {
			event := &adk.AgentEvent{
				Err: errors.New("story feedback is nil"),
			}
			gen.Send(event)
			return
		}

		event := &adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: sessionState.StoryFeedback,
					},
				},
			},
		}
		gen.Send(event)
	}()

	return iter
}
