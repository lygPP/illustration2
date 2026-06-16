package ill_agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"illustration2/internal/utils"
	"illustration2/internal/volc"
	"log"
	"path/filepath"
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

type chapterMediaResult struct {
	chapter  int
	videoURL string
	err      error
}

func NewChapterVideoGenerateAgent(ctx context.Context) adk.Agent {
	a := ChapterVideoGenerateAgent{
		AgentName: "章节视频生成助手",
		AgentDesc: "一个可以基于全局角色参考图并发生成章节视频的agent",
		ModelName: "ep-20260608235054-hwn67",
		ArkClient: volc.NewArkClientWithTimeout(time.Duration(envInt("AGENT_VIDEO_HTTP_TIMEOUT_SECONDS", 1800)) * time.Second),
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
		if sessionState.Story == nil || len(sessionState.Story.Chapters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("story is empty, cannot generate chapter videos")})
			return
		}
		if len(sessionState.Characters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("characters are empty, cannot generate chapter videos")})
			return
		}
		if sessionState.ConfirmedImages == nil || len(sessionState.ConfirmedImages) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("confirmed first frame images are empty, cannot generate chapter videos")})
			return
		}

		promptByChapter := make(map[int]string, len(sessionState.ChapterVideoPrompts))
		for _, p := range sessionState.ChapterVideoPrompts {
			promptByChapter[p.ChapterIndex] = strings.TrimSpace(p.Prompt)
		}

		chapterIndices := make([]int, 0, len(sessionState.Story.Chapters))
		for idx := range sessionState.Story.Chapters {
			chapterIndices = append(chapterIndices, idx)
		}
		sort.Ints(chapterIndices)

		ctx2, cancel := context.WithCancel(ctx)
		defer cancel()

		resCh := make(chan chapterMediaResult, len(chapterIndices))
		var wg sync.WaitGroup
		wg.Add(len(chapterIndices))
		for _, idx := range chapterIndices {
			chapterIdx := idx
			go func() {
				defer wg.Done()

				basePrompt := promptByChapter[chapterIdx]
				if basePrompt == "" && sessionState.Story != nil && chapterIdx >= 0 && chapterIdx < len(sessionState.Story.Chapters) {
					c := sessionState.Story.Chapters[chapterIdx]
					basePrompt = strings.TrimSpace(c.Title) + "\n" + strings.TrimSpace(c.Content)
				}
				if basePrompt == "" && strings.TrimSpace(sessionState.Story.Theme) != "" {
					basePrompt = strings.TrimSpace(sessionState.Story.Theme)
				}
				if basePrompt == "" {
					resCh <- chapterMediaResult{chapter: chapterIdx, err: fmt.Errorf("chapter %d prompt is empty", chapterIdx)}
					cancel()
					return
				}

				videoPrompt := basePrompt + "\nAnimate from the provided first frame image. Keep the opening frame visually consistent with the image. Smooth motion, cinematic lighting, consistent style, no on-screen text, no subtitles, no logos, no watermark."
				firstFrameImages := sessionState.ConfirmedImages[chapterIdx]
				if len(firstFrameImages) == 0 {
					resCh <- chapterMediaResult{chapter: chapterIdx, err: fmt.Errorf("chapter %d has no confirmed first frame image", chapterIdx+1)}
					cancel()
					return
				}
				if existingURL := strings.TrimSpace(sessionState.ChapterVideoURLs[chapterIdx]); existingURL != "" {
					resCh <- chapterMediaResult{
						chapter:  chapterIdx,
						videoURL: existingURL,
					}
					return
				}

				videoURL, err := r.generateChapterVideo(ctx2, videoPrompt, firstFrameImages[0], chapterIdx)
				if err != nil {
					resCh <- chapterMediaResult{chapter: chapterIdx, err: err}
					cancel()
					return
				}
				resCh <- chapterMediaResult{
					chapter:  chapterIdx,
					videoURL: videoURL,
				}
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
			chapterVideoURLs[r.chapter] = r.videoURL
		}

		sessionState.ChapterVideoURLs = chapterVideoURLs
		sessionState.VideoURL = ""
		sessionState.State = "chapter_video_generate"
		SaveSessionState(ctx, sessionState)

		infoList := make([]map[string]interface{}, 0, len(chapterIndices)+1)
		infoList = append(infoList, map[string]interface{}{
			"text": "已生成每个章节视频，下一步请选择音色生成解说音频：",
		})
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

func (r ChapterVideoGenerateAgent) generateChapterVideo(ctx context.Context, videoPrompt, firstFrameURL string, chapterIdx int) (string, error) {
	if r.ArkClient.Mock {
		outputPath := filepath.Join("resource", "agent_video", fmt.Sprintf("%s_chapter_%02d_mock_%s.mp4", safeFileStem(GetSessionID(ctx)), chapterIdx+1, time.Now().Format("20060102_150405")))
		if err := utils.CreateSilentVideo(ctx, outputPath, 10); err != nil {
			return "", fmt.Errorf("create mock video failed for chapter %d: %w", chapterIdx+1, err)
		}
		return utils.ResourceURL(outputPath), nil
	}

	videoParams := volc.VideoTaskParams{
		Model:         r.ModelName,
		Prompt:        videoPrompt,
		FirstFrameURL: firstFrameURL,
		GenerateAudio: boolPtr(false),
		Duration:      10,
	}

	taskID, err := r.ArkClient.CreateVideoTask(ctx, videoParams)
	if err != nil {
		return "", fmt.Errorf("create task failed for chapter %d: %w", chapterIdx+1, err)
	}
	log.Printf("chapter %d video task created: %s\n", chapterIdx+1, taskID)

	var status string
	var videoURL string
	maxAttempts := envInt("AGENT_VIDEO_POLL_MAX_ATTEMPTS", 360)
	attempts := 0
	for attempts < maxAttempts {
		status, videoURL, err = r.ArkClient.GetVideoTask(ctx, taskID)
		if err != nil {
			return "", fmt.Errorf("get task failed for chapter %d: %w", chapterIdx+1, err)
		}
		if status == "succeeded" && videoURL != "" {
			log.Printf("chapter %d video generated: %s\n", chapterIdx+1, videoURL)
			return videoURL, nil
		}
		if status == "failed" {
			return "", fmt.Errorf("video generation failed for chapter %d", chapterIdx+1)
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("video generation canceled for chapter %d: %w", chapterIdx+1, ctx.Err())
		case <-time.After(time.Duration(envInt("AGENT_VIDEO_POLL_INTERVAL_SECONDS", 5)) * time.Second):
		}
		attempts++
	}

	return "", fmt.Errorf("video generation timeout for chapter %d", chapterIdx+1)
}

func boolPtr(value bool) *bool {
	return &value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safeFileStem(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "story"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	stem := strings.Trim(b.String(), "_")
	if stem == "" {
		return "story"
	}
	if len(stem) > 48 {
		return stem[:48]
	}
	return stem
}
