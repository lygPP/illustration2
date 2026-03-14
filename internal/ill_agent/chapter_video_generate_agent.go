package ill_agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"illustration2/internal/volc"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type ChapterVideoGenerateAgent struct {
	AgentName string
	AgentDesc string
	ModelName string
	ArkClient *volc.ArkClient
}

func NewChapterVideoGenerateAgent(ctx context.Context) adk.Agent {
	a := ChapterVideoGenerateAgent{
		AgentName: "章节视频生成助手",
		AgentDesc: "一个可以基于每章首帧图并发生成视频的agent",
		ModelName: "ep-20260305130909-qnwqm",
		ArkClient: volc.NewArkClientWithTimeout(300 * time.Second),
	}
	return a
}

func (r ChapterVideoGenerateAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r ChapterVideoGenerateAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r ChapterVideoGenerateAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		if sessionState.GeneratedImages == nil || len(sessionState.GeneratedImages) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("no generated images found, cannot generate chapter videos")})
			return
		}

		promptByChapter := make(map[int]string, len(sessionState.ChapterVideoPrompts))
		for _, p := range sessionState.ChapterVideoPrompts {
			promptByChapter[p.ChapterIndex] = strings.TrimSpace(p.Prompt)
		}

		var chapterIndices []int
		for idx := range sessionState.GeneratedImages {
			chapterIndices = append(chapterIndices, idx)
		}
		sort.Ints(chapterIndices)

		type res struct {
			chapter int
			url     string
			err     error
		}

		ctx2, cancel := context.WithCancel(ctx)
		defer cancel()

		resCh := make(chan res, len(chapterIndices))
		var wg sync.WaitGroup
		wg.Add(len(chapterIndices))
		for _, idx := range chapterIndices {
			chapterIdx := idx
			go func() {
				defer wg.Done()

				images := sessionState.GeneratedImages[chapterIdx]
				if len(images) == 0 {
					resCh <- res{chapter: chapterIdx, err: fmt.Errorf("chapter %d has no images", chapterIdx)}
					cancel()
					return
				}
				firstFrameURL := images[0]
				if strings.TrimSpace(firstFrameURL) == "" {
					resCh <- res{chapter: chapterIdx, err: fmt.Errorf("chapter %d first frame url is empty", chapterIdx)}
					cancel()
					return
				}

				basePrompt := promptByChapter[chapterIdx]
				if basePrompt == "" && sessionState.Story != nil && chapterIdx >= 0 && chapterIdx < len(sessionState.Story.Chapters) {
					c := sessionState.Story.Chapters[chapterIdx]
					basePrompt = strings.TrimSpace(c.Title) + "\n" + strings.TrimSpace(c.Content)
				}
				if basePrompt == "" && strings.TrimSpace(sessionState.Story.Theme) != "" {
					basePrompt = strings.TrimSpace(sessionState.Story.Theme)
				}
				if basePrompt == "" {
					resCh <- res{chapter: chapterIdx, err: fmt.Errorf("chapter %d prompt is empty", chapterIdx)}
					cancel()
					return
				}

				// videoPrompt := basePrompt + "\nAnimate from the provided first frame image. Duration 8 seconds. 16:9, 24fps. Smooth motion, cinematic lighting, consistent style, no on-screen text, no subtitles, no logos, no watermark."
				videoPrompt := basePrompt

				videoParams := volc.VideoTaskParams{
					Model:         r.ModelName,
					Prompt:        videoPrompt,
					FirstFrameURL: firstFrameURL,
					Duration:      8,
				}

				taskID, err := r.ArkClient.CreateVideoTask(ctx2, videoParams)
				if err != nil {
					resCh <- res{chapter: chapterIdx, err: fmt.Errorf("create task failed for chapter %d: %w", chapterIdx, err)}
					cancel()
					return
				}

				var status string
				var videoURL string
				maxAttempts := 120
				attempts := 0
				for attempts < maxAttempts {
					status, videoURL, err = r.ArkClient.GetVideoTask(ctx2, taskID)
					if err != nil {
						resCh <- res{chapter: chapterIdx, err: fmt.Errorf("get task failed for chapter %d: %w", chapterIdx, err)}
						cancel()
						return
					}
					if status == "succeeded" && videoURL != "" {
						resCh <- res{chapter: chapterIdx, url: videoURL}
						return
					}
					if status == "failed" {
						resCh <- res{chapter: chapterIdx, err: fmt.Errorf("video generation failed for chapter %d", chapterIdx)}
						cancel()
						return
					}
					time.Sleep(5 * time.Second)
					attempts++
				}

				resCh <- res{chapter: chapterIdx, err: fmt.Errorf("video generation timeout for chapter %d", chapterIdx)}
				cancel()
			}()
		}

		wg.Wait()
		close(resCh)

		chapterVideoURLs := make(map[int]string, len(chapterIndices))
		for r := range resCh {
			if r.err != nil {
				gen.Send(&adk.AgentEvent{Err: r.err})
				return
			}
			chapterVideoURLs[r.chapter] = r.url
		}

		sessionState.ChapterVideoURLs = chapterVideoURLs
		sessionState.State = "chapter_video_generate"
		SaveSessionState(ctx, sessionState)

		infoList := make([]map[string]interface{}, 0, len(chapterIndices))
		for _, idx := range chapterIndices {
			if url := chapterVideoURLs[idx]; url != "" {
				infoList = append(infoList, map[string]interface{}{
					"text":      fmt.Sprintf("第%d章视频：", idx+1),
					"videoUrls": []string{url},
				})
			}
		}
		data, _ := json.Marshal(infoList)

		log.Printf("chapterVideoURLs: %+v\n", chapterVideoURLs)

		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: string(data),
					},
				},
			},
		})
	}()

	return iter
}
