package ill_agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"illustration2/internal/indextts"
	"illustration2/internal/usage"
	"illustration2/internal/utils"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type ChapterNarrationAgent struct {
	AgentName string
	AgentDesc string
}

type chapterNarrationResult struct {
	chapter          int
	videoURL         string
	audioURL         string
	narratedVideoURL string
	err              error
}

func NewChapterNarrationAgent(ctx context.Context) adk.Agent {
	return ChapterNarrationAgent{
		AgentName: "章节语音合成助手",
		AgentDesc: "在章节视频生成后合成章节解说音频，并完成章节音视频合成和整片拼接",
	}
}

func (r ChapterNarrationAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r ChapterNarrationAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r ChapterNarrationAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		if ShouldSkipStage(sessionState, StageNarration) {
			gen.Send(StageSkippedEvent(r.AgentName))
			return
		}
		if sessionState.Story == nil || len(sessionState.Story.Chapters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("story is empty, cannot synthesize chapter narrations")})
			return
		}
		if len(sessionState.ChapterVideoURLs) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("chapter videos are empty, cannot synthesize chapter narrations")})
			return
		}
		if strings.TrimSpace(sessionState.SelectedVoiceType) == "" {
			gen.Send(&adk.AgentEvent{Err: errors.New("selected voice is empty, cannot synthesize chapter narrations")})
			return
		}

		chapterIndices := make([]int, 0, len(sessionState.Story.Chapters))
		for idx := range sessionState.Story.Chapters {
			chapterIndices = append(chapterIndices, idx)
		}
		sort.Ints(chapterIndices)

		ctx2, cancel := context.WithCancel(ctx)
		defer cancel()

		narrationSlots := make(chan struct{}, narrationConcurrencyLimit())
		resCh := make(chan chapterNarrationResult, len(chapterIndices))
		var wg sync.WaitGroup
		wg.Add(len(chapterIndices))
		for _, idx := range chapterIndices {
			chapterIdx := idx
			go func() {
				defer wg.Done()

				videoURL := strings.TrimSpace(sessionState.ChapterVideoURLs[chapterIdx])
				if videoURL == "" {
					resCh <- chapterNarrationResult{chapter: chapterIdx, err: fmt.Errorf("chapter %d video is empty", chapterIdx+1)}
					cancel()
					return
				}
				if existingURL := strings.TrimSpace(sessionState.NarratedChapterVideoURLs[chapterIdx]); existingURL != "" {
					resCh <- chapterNarrationResult{
						chapter:          chapterIdx,
						videoURL:         videoURL,
						audioURL:         sessionState.ChapterAudioURLs[chapterIdx],
						narratedVideoURL: existingURL,
					}
					return
				}

				chapter := sessionState.Story.Chapters[chapterIdx]
				if err := acquireNarrationSlot(ctx2, narrationSlots); err != nil {
					resCh <- chapterNarrationResult{chapter: chapterIdx, err: fmt.Errorf("synthesize narration canceled for chapter %d: %w", chapterIdx+1, err)}
					cancel()
					return
				}
				audioURL, audioPath, audioErr := r.synthesizeChapterNarration(ctx2, sessionState.SelectedVoiceType, chapter.Content, chapterIdx)
				releaseNarrationSlot(narrationSlots)
				if audioErr != nil {
					log.Printf("chapter %d narration failed, continue with silent video: %v\n", chapterIdx+1, audioErr)
					resCh <- chapterNarrationResult{
						chapter:          chapterIdx,
						videoURL:         videoURL,
						narratedVideoURL: videoURL,
					}
					return
				}

				narratedPath := filepath.Join("resource", "agent_video", fmt.Sprintf("%s_chapter_%02d_narrated_%s.mp4", safeFileStem(GetSessionID(ctx2)), chapterIdx+1, time.Now().Format("20060102_150405")))
				if err := utils.MuxVideoWithAudioFromURL(ctx2, videoURL, audioPath, narratedPath); err != nil {
					log.Printf("chapter %d mux failed, continue with silent video: %v\n", chapterIdx+1, err)
					resCh <- chapterNarrationResult{
						chapter:          chapterIdx,
						videoURL:         videoURL,
						audioURL:         audioURL,
						narratedVideoURL: videoURL,
					}
					return
				}

				resCh <- chapterNarrationResult{
					chapter:          chapterIdx,
					videoURL:         videoURL,
					audioURL:         audioURL,
					narratedVideoURL: utils.ResourceURL(narratedPath),
				}
			}()
		}

		wg.Wait()
		close(resCh)

		chapterAudioURLs := make(map[int]string, len(chapterIndices))
		narratedChapterVideoURLs := make(map[int]string, len(chapterIndices))
		for r := range resCh {
			if r.err != nil {
				gen.Send(&adk.AgentEvent{Err: r.err})
				return
			}
			chapterAudioURLs[r.chapter] = r.audioURL
			narratedChapterVideoURLs[r.chapter] = r.narratedVideoURL
		}

		finalVideoURL, err := buildFinalVideo(ctx, sessionState, chapterIndices, narratedChapterVideoURLs)
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}

		sessionState.ChapterAudioURLs = chapterAudioURLs
		sessionState.NarratedChapterVideoURLs = narratedChapterVideoURLs
		sessionState.VideoURL = finalVideoURL
		sessionState.State = "chapter_narration_generate"
		SaveSessionState(ctx, sessionState)

		infoList := make([]map[string]interface{}, 0, len(chapterIndices)+2)
		infoList = append(infoList, map[string]interface{}{
			"text":      "已完成章节音视频合成和完整视频拼接：",
			"videoUrls": []string{finalVideoURL},
		})
		for _, idx := range chapterIndices {
			if url := narratedChapterVideoURLs[idx]; url != "" {
				item := map[string]interface{}{
					"text":      fmt.Sprintf("第%d章视频：", idx+1),
					"videoUrls": []string{url},
				}
				if chapterAudioURLs[idx] != "" {
					item["audioUrls"] = []string{chapterAudioURLs[idx]}
				}
				infoList = append(infoList, item)
			}
		}
		data, _ := json.Marshal(infoList)
		log.Printf("chapterAudioURLs: %+v narratedChapterVideoURLs: %+v finalVideoURL: %s\n", chapterAudioURLs, narratedChapterVideoURLs, finalVideoURL)

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

func narrationConcurrencyLimit() int {
	limit := envInt("AGENT_NARRATION_CONCURRENCY", 2)
	if limit < 1 {
		return 1
	}
	return limit
}

func acquireNarrationSlot(ctx context.Context, slots chan struct{}) error {
	select {
	case slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func releaseNarrationSlot(slots chan struct{}) {
	select {
	case <-slots:
	default:
	}
}

func (r ChapterNarrationAgent) synthesizeChapterNarration(ctx context.Context, voiceType, text string, chapterIdx int) (string, string, error) {
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
	_ = usage.Record(ctx, "index-tts", 0, 0)
	return utils.ResourceURL(audioPath), audioPath, nil
}

func buildFinalVideo(ctx context.Context, sessionState *IllustrationSessionState, chapterIndices []int, videoURLs map[int]string) (string, error) {
	if len(chapterIndices) == 0 {
		return "", errors.New("no chapter videos generated")
	}
	if len(chapterIndices) == 1 {
		return videoURLs[chapterIndices[0]], nil
	}

	videoURLList := make([]string, 0, len(chapterIndices))
	for _, idx := range chapterIndices {
		if videoURLs[idx] == "" {
			return "", fmt.Errorf("chapter %d video is empty", idx+1)
		}
		videoURLList = append(videoURLList, videoURLs[idx])
	}

	theme := "story"
	if sessionState.Story != nil && strings.TrimSpace(sessionState.Story.Theme) != "" {
		theme = safeFileStem(sessionState.Story.Theme)
	}
	outputPath := filepath.Join("resource", "agent_video", fmt.Sprintf("%s_complete_%s.mp4", theme, time.Now().Format("20060102_150405")))
	if err := utils.ConcatVideosFromURLs(ctx, videoURLList, outputPath); err != nil {
		return "", fmt.Errorf("concat chapter videos failed: %w", err)
	}
	return utils.ResourceURL(outputPath), nil
}
