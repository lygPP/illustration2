package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type ImageReviewAgent struct {
	AgentName string
	AgentDesc string
}

func NewImageReviewAgent(ctx context.Context) adk.Agent {
	return ImageReviewAgent{
		AgentName: "图片审核agent",
		AgentDesc: "一个可以审核图片是否符合要求的agent",
	}
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

		sessionState := GetSessionState(ctx)
		if sessionState.GeneratedImages == nil {
			event := &adk.AgentEvent{
				Err: errors.New("generated_images not found in session"),
			}
			gen.Send(event)
			return
		}

		sessionState.State = "image_review"
		SaveSessionState(ctx, sessionState)

		info := "Image content to review: \n"
		for i, urls := range sessionState.GeneratedImages {
			info = info + fmt.Sprintf("第%d章节组图：\n", i)
			for _, url := range urls {
				info = info + fmt.Sprintf("%s\n", url)
			}
		}
		info = info + fmt.Sprintf("\nIf you think the images are good as it is, please reply with \"ok\". \nOtherwise, please provide your feedback.")
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

		if info.ResumeData == nil {
			event := &adk.AgentEvent{
				Err: errors.New("image_review agent receives nil resume data"),
			}
			gen.Send(event)
			return
		}

		feedback, ok := info.ResumeData.(string)
		if !ok {
			event := &adk.AgentEvent{
				Err: errors.New("image_review agent receives invalid resume data"),
			}
			gen.Send(event)
			return
		}

		sessionState := GetSessionState(ctx)
		if strings.ToLower(feedback) != "ok" {
			sessionState.NeedToEditImages = true
			sessionState.ImageFeedback = feedback
		} else {
			sessionState.NeedToEditImages = false
		}
		SaveSessionState(ctx, sessionState)

		if !sessionState.NeedToEditImages {
			event := &adk.AgentEvent{
				Action: adk.NewBreakLoopAction(r.AgentName),
				// Action: adk.NewExitAction(),
			}
			gen.Send(event)
			return
		}

		if sessionState.ImageFeedback == "" {
			event := &adk.AgentEvent{
				Err: errors.New("image feedback is nil"),
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
						Content: sessionState.ImageFeedback,
					},
				},
			},
		}
		gen.Send(event)
	}()

	return iter
}
