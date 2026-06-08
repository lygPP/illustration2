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
		AgentName: "图片内容审核助手",
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
		if sessionState.Story == nil || len(sessionState.Story.Chapters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("story is empty, cannot review first frame image")})
			return
		}
		chapterIndex := sessionState.CurrentImageChapter
		if chapterIndex < 0 || chapterIndex >= len(sessionState.Story.Chapters) {
			gen.Send(&adk.AgentEvent{Err: errors.New("current image chapter is out of range")})
			return
		}
		urls := sessionState.GeneratedImages[chapterIndex]
		if len(urls) == 0 {
			gen.Send(&adk.AgentEvent{Err: fmt.Errorf("chapter %d first frame image not found", chapterIndex+1)})
			return
		}

		sessionState.State = "image_review"
		SaveSessionState(ctx, sessionState)

		infoList := make([]map[string]interface{}, 0)
		infoList = append(infoList, map[string]interface{}{
			"text": fmt.Sprintf("已生成第%d章首帧图，请审核：", chapterIndex+1),
		})
		chapter := sessionState.Story.Chapters[chapterIndex]
		infoList = append(infoList, map[string]interface{}{
			"chapterIndex":   chapterIndex,
			"chapterTitle":   chapter.Title,
			"chapterContent": chapter.Content,
		})
		infoList = append(infoList, map[string]interface{}{
			"text":      fmt.Sprintf("第%d章首帧图：", chapterIndex+1),
			"imageUrls": urls,
		})
		infoList = append(infoList, map[string]interface{}{
			"text": "如果该章节首帧图符合要求，请回复ok。否则提供反馈，我会先重生成本章节，再继续下一章。",
		})
		event := adk.StatefulInterrupt(ctx, infoList, sessionState.State)
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
			sessionState.ImageFeedback = ""
		}
		SaveSessionState(ctx, sessionState)

		if !sessionState.NeedToEditImages {
			if sessionState.ConfirmedImages == nil {
				sessionState.ConfirmedImages = make(map[int][]string)
			}
			chapterIndex := sessionState.CurrentImageChapter
			if len(sessionState.GeneratedImages[chapterIndex]) == 0 {
				gen.Send(&adk.AgentEvent{Err: fmt.Errorf("chapter %d first frame image not found", chapterIndex+1)})
				return
			}
			sessionState.ConfirmedImages[chapterIndex] = sessionState.GeneratedImages[chapterIndex]
			if sessionState.Story == nil || chapterIndex >= len(sessionState.Story.Chapters)-1 {
				SaveSessionState(ctx, sessionState)
				event := &adk.AgentEvent{
					Action: adk.NewBreakLoopAction(r.AgentName),
				}
				gen.Send(event)
				return
			}
			sessionState.CurrentImageChapter = chapterIndex + 1
			SaveSessionState(ctx, sessionState)
			gen.Send(&adk.AgentEvent{
				Output: &adk.AgentOutput{
					MessageOutput: &adk.MessageVariant{
						IsStreaming: false,
						Message: &schema.Message{
							Role:    schema.Assistant,
							Content: fmt.Sprintf("第%d章首帧图已确认，继续生成第%d章首帧图", chapterIndex+1, chapterIndex+2),
						},
					},
				},
			})
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
