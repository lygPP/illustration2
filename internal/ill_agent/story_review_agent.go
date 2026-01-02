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

type StoryReviewAgent struct {
	AgentName string
	AgentDesc string
}

func (r StoryReviewAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r StoryReviewAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r StoryReviewAgent) Run(ctx context.Context, input *adk.AgentInput,
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

		storyChapters := make([]model.StoryChapter, 0)
		chapters := strings.Split(contentToReview.(string), "\n\n")
		for _, chapter := range chapters {
			chapterParts := strings.Split(chapter, "\n")
			storyChapters = append(storyChapters, model.StoryChapter{
				Title:   chapterParts[0],
				Content: chapterParts[1],
			})
		}
		sessionState := GetSessionState(ctx)
		sessionState.Story.Chapters = storyChapters
		sessionState.State = "story_review"
		SaveSessionState(ctx, sessionState)

		info := "Story content to review: \n"
		for _, chapter := range sessionState.Story.Chapters {
			info = info + fmt.Sprintf("%s\n%s\n", chapter.Title, chapter.Content)
		}
		info = info + fmt.Sprintf("\nIf you think the content is good as it is, please reply with \"ok\". \nOtherwise, please provide your feedback.")
		event := adk.StatefulInterrupt(ctx, info, sessionState.State)
		gen.Send(event)
	}()

	return iter
}

func (r StoryReviewAgent) Resume(ctx context.Context, info *adk.ResumeInfo,
	opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()
		// if !info.IsResumeTarget { // not explicitly resumed, interrupt with the same review content again
		// 	sessionState := GetSessionState(ctx)
		// 	info := "Story content to review: \n"
		// 	for i, chapter := range sessionState.Story.Chapters {
		// 		info = info + fmt.Sprintf("第%d章: %s\n%s\n", i+1, chapter.Title, chapter.Content)
		// 	}
		// 	info = info + fmt.Sprintf("\nIf you think the content is good as it is, please reply with \"ok\". \nOtherwise, please provide your feedback.")
		// 	event := adk.StatefulInterrupt(ctx, info, sessionState.State)
		// 	gen.Send(event)
		// 	return
		// }

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
		if strings.ToLower(feedback) != "ok" {
			sessionState.NeedToEditStory = true
			sessionState.StoryFeedback = feedback
		} else {
			sessionState.NeedToEditStory = false
		}
		SaveSessionState(ctx, sessionState)

		if !sessionState.NeedToEditStory {
			event := &adk.AgentEvent{
				Action: adk.NewBreakLoopAction(r.AgentName),
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
