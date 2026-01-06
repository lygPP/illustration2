package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/volc"
	"log"
	"sort"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type VideoGenerateAgent struct {
	AgentName string
	AgentDesc string
	ModelName string
	ArkClient *volc.ArkClient
}

func NewVideoGenerateAgent(ctx context.Context) adk.Agent {
	a := VideoGenerateAgent{
		AgentName: "VideoGenerateAgent",
		AgentDesc: "一个可以根据生成的图片创建视频的agent",
		ModelName: "ep-20260107003549-kcrmk",
		ArkClient: volc.NewArkClientWithTimeout(300 * time.Second), // 视频生成可能需要更长时间
	}
	return a
}

func (r VideoGenerateAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r VideoGenerateAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r VideoGenerateAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)

		// 检查是否有生成的图片
		if sessionState.GeneratedImages == nil || len(sessionState.GeneratedImages) == 0 {
			event := &adk.AgentEvent{
				Err: errors.New("no generated images found, cannot generate video"),
			}
			gen.Send(event)
			return
		}

		// 准备视频生成的提示词和图片
		var videoPrompt string
		var referenceImages []string

		// 从故事和图片提示词中生成视频描述
		if sessionState.Story != nil {
			videoPrompt = fmt.Sprintf("根据以下故事生成一个连贯的视频：%s\n", sessionState.Story.Theme)
			for i, chapter := range sessionState.Story.Chapters {
				videoPrompt += fmt.Sprintf("第%d章：%s\n%s\n", i+1, chapter.Title, chapter.Content)
			}
		}

		// 收集所有生成的图片作为参考
		var chapterIndices []int
		for idx := range sessionState.GeneratedImages {
			chapterIndices = append(chapterIndices, idx)
		}
		// 按章节索引从小到大排序
		sort.Ints(chapterIndices)
		// 按排序后的顺序遍历图片
		for _, idx := range chapterIndices {
			images := sessionState.GeneratedImages[idx]
			referenceImages = append(referenceImages, images...)
		}

		// 调用视频生成API
		videoParams := volc.VideoTaskParams{
			Model:              r.ModelName,
			Prompt:             videoPrompt,
			ReferenceImageURLs: referenceImages,
		}

		// 创建视频任务
		taskID, err := r.ArkClient.CreateVideoTask(ctx, videoParams)
		if err != nil {
			log.Fatal(fmt.Errorf("video task creation failed: %+v", err))
			event := &adk.AgentEvent{
				Err: errors.New("video task creation failed"),
			}
			gen.Send(event)
			return
		}

		// 轮询视频任务状态
		var status string
		var videoURL string
		maxAttempts := 60 // 最多轮询60次
		attempts := 0

		for attempts < maxAttempts {
			status, videoURL, err = r.ArkClient.GetVideoTask(ctx, taskID)
			if err != nil {
				log.Fatal(fmt.Errorf("failed to get video task status: %+v", err))
				event := &adk.AgentEvent{
					Err: errors.New("failed to get video task status"),
				}
				gen.Send(event)
				return
			}

			if status == "succeeded" && videoURL != "" {
				break
			}

			if status == "failed" {
				event := &adk.AgentEvent{
					Err: errors.New("video generation failed"),
				}
				gen.Send(event)
				return
			}

			// 等待5秒后再次轮询
			time.Sleep(5 * time.Second)
			attempts++
		}

		if attempts >= maxAttempts {
			event := &adk.AgentEvent{
				Err: errors.New("video generation timeout"),
			}
			gen.Send(event)
			return
		}

		// 保存视频URL到会话状态
		sessionState.VideoURL = videoURL
		sessionState.State = "video_generate"
		SaveSessionState(ctx, sessionState)

		fmt.Printf("Generated video URL: %s\n", videoURL)

		event := &adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: fmt.Sprintf("视频生成完成！视频URL: %s", videoURL),
					},
				},
			},
		}
		gen.Send(event)
	}()

	return iter
}
