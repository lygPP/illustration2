package ill_agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"illustration2/internal/indextts"
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
	chapter           int
	videoURL          string
	audioURL          string
	audioPath         string
	narratedVideoURL  string
	narratedVideoPath string
	err               error
}

func NewChapterVideoGenerateAgent(ctx context.Context) adk.Agent {
	a := ChapterVideoGenerateAgent{
		AgentName: "章节视频生成助手",
		AgentDesc: "一个可以基于全局角色参考图并发生成章节视频的agent",
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
		if strings.TrimSpace(sessionState.SelectedVoiceType) == "" {
			gen.Send(&adk.AgentEvent{Err: errors.New("selected voice is empty, cannot synthesize chapter narrations")})
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

				var videoURL string
				var audioURL string
				var audioPath string
				var videoErr error
				var audioErr error
				var chapterWG sync.WaitGroup
				chapterWG.Add(2)
				go func() {
					defer chapterWG.Done()
					videoURL, videoErr = r.generateChapterVideo(ctx2, videoPrompt, firstFrameImages[0], chapterIdx)
				}()
				go func() {
					defer chapterWG.Done()
					chapter := sessionState.Story.Chapters[chapterIdx]
					audioURL, audioPath, audioErr = r.synthesizeChapterNarration(ctx2, sessionState.SelectedVoiceType, chapter.Content, chapterIdx)
				}()
				chapterWG.Wait()
				if videoErr != nil {
					resCh <- chapterMediaResult{chapter: chapterIdx, err: videoErr}
					cancel()
					return
				}
				if audioErr != nil {
					resCh <- chapterMediaResult{chapter: chapterIdx, err: audioErr}
					cancel()
					return
				}

				narratedPath := filepath.Join("resource", "agent_video", fmt.Sprintf("%s_chapter_%02d_narrated_%s.mp4", safeFileStem(GetSessionID(ctx2)), chapterIdx+1, time.Now().Format("20060102_150405")))
				if err := utils.MuxVideoWithAudioFromURL(ctx2, videoURL, audioPath, narratedPath); err != nil {
					resCh <- chapterMediaResult{chapter: chapterIdx, err: fmt.Errorf("mux video and narration failed for chapter %d: %w", chapterIdx+1, err)}
					cancel()
					return
				}

				resCh <- chapterMediaResult{
					chapter:           chapterIdx,
					videoURL:          videoURL,
					audioURL:          audioURL,
					audioPath:         audioPath,
					narratedVideoURL:  utils.ResourceURL(narratedPath),
					narratedVideoPath: narratedPath,
				}
			}()
		}

		wg.Wait()
		close(resCh)

		chapterVideoURLs := make(map[int]string, len(chapterIndices))
		chapterAudioURLs := make(map[int]string, len(chapterIndices))
		narratedChapterVideoURLs := make(map[int]string, len(chapterIndices))
		for r := range resCh {
			if r.err != nil {
				gen.Send(&adk.AgentEvent{Err: r.err})
				return
			}
			chapterVideoURLs[r.chapter] = r.videoURL
			chapterAudioURLs[r.chapter] = r.audioURL
			narratedChapterVideoURLs[r.chapter] = r.narratedVideoURL
		}

		finalVideoURL, err := buildFinalNarratedVideo(ctx, sessionState, chapterIndices, narratedChapterVideoURLs)
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}

		sessionState.ChapterVideoURLs = chapterVideoURLs
		sessionState.ChapterAudioURLs = chapterAudioURLs
		sessionState.NarratedChapterVideoURLs = narratedChapterVideoURLs
		sessionState.VideoURL = finalVideoURL
		sessionState.State = "chapter_video_generate"
		SaveSessionState(ctx, sessionState)

		infoList := make([]map[string]interface{}, 0, len(chapterIndices)+2)
		infoList = append(infoList, map[string]interface{}{
			"text":      "已生成完整解说视频：",
			"videoUrls": []string{finalVideoURL},
		})
		for _, idx := range chapterIndices {
			if url := narratedChapterVideoURLs[idx]; url != "" {
				infoList = append(infoList, map[string]interface{}{
					"text":      fmt.Sprintf("第%d章带解说视频：", idx+1),
					"videoUrls": []string{url},
					"audioUrls": []string{chapterAudioURLs[idx]},
				})
			}
		}
		data, _ := json.Marshal(infoList)

		log.Printf("chapterVideoURLs: %+v narratedChapterVideoURLs: %+v finalVideoURL: %s\n", chapterVideoURLs, narratedChapterVideoURLs, finalVideoURL)

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

	var status string
	var videoURL string
	maxAttempts := 120
	attempts := 0
	for attempts < maxAttempts {
		status, videoURL, err = r.ArkClient.GetVideoTask(ctx, taskID)
		if err != nil {
			return "", fmt.Errorf("get task failed for chapter %d: %w", chapterIdx+1, err)
		}
		if status == "succeeded" && videoURL != "" {
			return videoURL, nil
		}
		if status == "failed" {
			return "", fmt.Errorf("video generation failed for chapter %d", chapterIdx+1)
		}
		time.Sleep(5 * time.Second)
		attempts++
	}

	return "", fmt.Errorf("video generation timeout for chapter %d", chapterIdx+1)
}

func boolPtr(value bool) *bool {
	return &value
}

func (r ChapterVideoGenerateAgent) synthesizeChapterNarration(ctx context.Context, voiceType, text string, chapterIdx int) (string, string, error) {
	audio, err := indextts.NewClientFromEnv().Synthesize(ctx, indextts.SynthesisParams{
		Text:              strings.TrimSpace(text),
		ReferenceAudioURL: strings.TrimSpace(voiceType),
	})
	if err != nil {
		return "", "", fmt.Errorf("synthesize narration failed for chapter %d: %w", chapterIdx+1, err)
	}
	audioPath := filepath.Join("resource", "index_tts", "chapters", fmt.Sprintf("%s_chapter_%02d_%s.mp3", safeFileStem(GetSessionID(ctx)), chapterIdx+1, time.Now().Format("20060102_150405")))
	if err := utils.SaveAudioFile(audio, audioPath); err != nil {
		return "", "", fmt.Errorf("save narration failed for chapter %d: %w", chapterIdx+1, err)
	}
	return utils.ResourceURL(audioPath), audioPath, nil
}

func buildFinalNarratedVideo(ctx context.Context, sessionState *IllustrationSessionState, chapterIndices []int, narratedURLs map[int]string) (string, error) {
	if len(chapterIndices) == 0 {
		return "", errors.New("no chapter videos generated")
	}
	if len(chapterIndices) == 1 {
		return narratedURLs[chapterIndices[0]], nil
	}

	videoURLList := make([]string, 0, len(chapterIndices))
	for _, idx := range chapterIndices {
		if narratedURLs[idx] == "" {
			return "", fmt.Errorf("chapter %d narrated video is empty", idx+1)
		}
		videoURLList = append(videoURLList, narratedURLs[idx])
	}

	theme := "story"
	if sessionState.Story != nil && strings.TrimSpace(sessionState.Story.Theme) != "" {
		theme = safeFileStem(sessionState.Story.Theme)
	}
	outputPath := filepath.Join("resource", "agent_video", fmt.Sprintf("%s_complete_%s.mp4", theme, time.Now().Format("20060102_150405")))
	if err := utils.ConcatVideosFromURLs(ctx, videoURLList, outputPath); err != nil {
		return "", fmt.Errorf("concat narrated videos failed: %w", err)
	}
	return utils.ResourceURL(outputPath), nil
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
