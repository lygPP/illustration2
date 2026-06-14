package ill_agent

import (
	"context"
	"encoding/json"
	"fmt"
	"illustration2/internal/auth"
	"illustration2/internal/model"
	"illustration2/internal/volc"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

const (
	StageCurrent        = "current"
	StageStory          = "story"
	StageCharacter      = "character"
	StageImage          = "image"
	StageVoiceSelection = "voice_selection"
	StageChapterVideo   = "chapter_video"
)

const restartPlanModelName = "ep-20260608120832-fq5kh"

var stageRank = map[string]int{
	StageStory:          0,
	StageCharacter:      1,
	StageImage:          2,
	StageVoiceSelection: 3,
	StageChapterVideo:   4,
}

type RestartPlan struct {
	ShouldRestart bool   `json:"should_restart"`
	Stage         string `json:"stage"`
	ChapterIndex  int    `json:"chapter_index"`
	Feedback      string `json:"feedback"`
	Reason        string `json:"reason"`
}

func ShouldSkipStage(state *IllustrationSessionState, stage string) bool {
	if state == nil {
		return false
	}
	target := NormalizeRestartStage(state.StartFromStage)
	if target == "" || target == StageCurrent {
		return false
	}
	targetRank, ok := stageRank[target]
	if !ok {
		return false
	}
	currentRank, ok := stageRank[NormalizeRestartStage(stage)]
	return ok && currentRank < targetRank
}

func StageSkippedEvent(stageName string) *adk.AgentEvent {
	return &adk.AgentEvent{
		Output: &adk.AgentOutput{
			MessageOutput: &adk.MessageVariant{
				IsStreaming: false,
				Message: &schema.Message{
					Role:    schema.Assistant,
					Content: fmt.Sprintf("%s skipped by restart cursor", stageName),
				},
			},
		},
	}
}

func NormalizeRestartStage(stage string) string {
	stage = strings.ToLower(strings.TrimSpace(stage))
	stage = strings.ReplaceAll(stage, "-", "_")
	stage = strings.ReplaceAll(stage, " ", "_")
	switch stage {
	case "", StageCurrent, "latest", "continue", "当前位置", "当前", "最新", "继续":
		return StageCurrent
	case StageStory, "story_generate", "story_review", "故事", "主题", "章节内容":
		return StageStory
	case StageCharacter, "characters", "character_generate", "character_review", "角色", "全局角色":
		return StageCharacter
	case StageImage, "images", "image_prompt", "image_generate", "image_review", "first_frame", "首帧", "图片", "画面":
		return StageImage
	case StageVoiceSelection, "voice", "voice_selected", "音色", "配音":
		return StageVoiceSelection
	case StageChapterVideo, "video", "chapter_video_prompt", "chapter_video_generate", "章节视频", "视频":
		return StageChapterVideo
	default:
		return ""
	}
}

func InferRestartPlan(ctx context.Context, userInput string, state *IllustrationSessionState, forceRestart bool) RestartPlan {
	input := strings.TrimSpace(userInput)
	latest := LatestRunnableStage(state)
	if input == "" {
		return normalizeRestartPlan(RestartPlan{
			ShouldRestart: forceRestart,
			Stage:         latest,
			ChapterIndex:  -1,
			Feedback:      input,
			Reason:        "empty input uses latest runnable stage",
		}, latest, input, forceRestart)
	}

	client := volc.NewArkClientDefault()
	if client.Mock {
		plan := inferRestartPlanHeuristic(input, latest)
		plan.ShouldRestart = forceRestart || plan.ShouldRestart
		return normalizeRestartPlan(plan, latest, input, forceRestart)
	}

	prompt := fmt.Sprintf(`You classify a user's Chinese natural-language feedback for an illustration generation workflow.

Workflow stages, in order:
- "story": story and chapter text generation
- "character": global character profiles and reference images
- "image": chapter first-frame image prompts, first-frame generation, and image review
- "voice_selection": narration voice selection
- "chapter_video": chapter video prompts, chapter videos, narration audio, and final merged video
- "current": continue from the latest incomplete/current position without intentionally jumping back

Return strict JSON only:
{
  "should_restart": true,
  "stage": "story|character|image|voice_selection|chapter_video|current",
  "chapter_index": -1,
  "feedback": "cleaned user feedback",
  "reason": "short reason in Chinese"
}

Rules:
- If the user says OK, confirms, selects a voice, or gives ordinary feedback for the currently interrupted step, set should_restart=false and stage="current".
- If the user asks to go back, restart, rerun, redo, regenerate from a specific workflow area, set should_restart=true and choose that stage.
- If the user mentions a chapter number for first-frame/image work, chapter_index must be zero-based. Otherwise use -1.
- If the user asks to continue from the current/latest position, use stage="current".

Latest runnable stage inferred from saved state: %s
Saved state summary:
%s

User feedback:
%s`, latest, summarizeRestartState(state), input)

	content, _, err := client.ChatJSONWithUsage(ctx, restartPlanModelName, prompt)
	if err != nil {
		plan := inferRestartPlanHeuristic(input, latest)
		plan.ShouldRestart = forceRestart || plan.ShouldRestart
		return normalizeRestartPlan(plan, latest, input, forceRestart)
	}
	var plan RestartPlan
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &plan); err != nil {
		plan = inferRestartPlanHeuristic(input, latest)
	}
	plan.ShouldRestart = forceRestart || plan.ShouldRestart
	return normalizeRestartPlan(plan, latest, input, forceRestart)
}

func ApplyRestartPlan(state *IllustrationSessionState, plan RestartPlan) {
	if state == nil {
		return
	}
	stage := NormalizeRestartStage(plan.Stage)
	if stage == "" || stage == StageCurrent {
		stage = LatestRunnableStage(state)
	}
	state.StartFromStage = stage
	state.StartFromChapter = plan.ChapterIndex
	state.RestartFeedback = strings.TrimSpace(plan.Feedback)
	state.RestartReason = strings.TrimSpace(plan.Reason)

	feedback := strings.TrimSpace(plan.Feedback)
	switch stage {
	case StageStory:
		state.StoryFeedback = feedback
		state.NeedToEditStory = feedback != ""
	case StageCharacter:
		state.CharacterFeedback = feedback
		state.NeedToEditCharacters = feedback != ""
	case StageImage:
		state.ImageFeedback = feedback
		state.NeedToEditImages = feedback != ""
		if plan.ChapterIndex >= 0 {
			state.CurrentImageChapter = plan.ChapterIndex
		} else {
			state.CurrentImageChapter = FirstUnconfirmedChapter(state)
		}
	case StageVoiceSelection:
		state.State = StageVoiceSelection
	case StageChapterVideo:
		state.State = StageChapterVideo
	}
}

func SessionStateFromAgentWork(work *auth.AgentWork) *IllustrationSessionState {
	state := &IllustrationSessionState{
		State:                    strings.TrimSpace(work.State),
		Story:                    cloneStory(work.Story),
		Characters:               cloneCharacterProfiles(work.Characters),
		ImagePrompts:             append([]model.ImagePrompt(nil), work.ImagePrompts...),
		GeneratedImages:          cloneStringSliceMap(work.GeneratedImages),
		ConfirmedImages:          cloneStringSliceMap(work.ConfirmedImages),
		CurrentImageChapter:      work.CurrentImageChapter,
		VideoPrompt:              work.VideoPrompt,
		ChapterVideoPrompts:      append([]model.VideoPrompt(nil), work.ChapterVideoPrompts...),
		ChapterVideoURLs:         cloneStringMap(work.ChapterVideoURLs),
		ChapterAudioURLs:         cloneStringMap(work.ChapterAudioURLs),
		NarratedChapterVideoURLs: cloneStringMap(work.NarratedChapterVideoURLs),
		VideoURL:                 work.VideoURL,
		SelectedVoiceID:          work.SelectedVoiceID,
		SelectedVoiceType:        work.SelectedVoiceType,
		SelectedVoiceName:        work.SelectedVoiceName,
		StartFromChapter:         -1,
	}
	if state.State == "" {
		state.State = LatestRunnableStage(state)
	}
	if state.Story == nil {
		state.Story = &model.Story{Theme: strings.TrimSpace(work.Theme)}
	} else if strings.TrimSpace(state.Story.Theme) == "" {
		state.Story.Theme = strings.TrimSpace(work.Theme)
	}
	if state.GeneratedImages == nil {
		state.GeneratedImages = make(map[int][]string)
	}
	if state.ConfirmedImages == nil {
		state.ConfirmedImages = make(map[int][]string)
	}
	if state.ChapterVideoURLs == nil {
		state.ChapterVideoURLs = make(map[int]string)
	}
	if state.ChapterAudioURLs == nil {
		state.ChapterAudioURLs = make(map[int]string)
	}
	if state.NarratedChapterVideoURLs == nil {
		state.NarratedChapterVideoURLs = make(map[int]string)
	}
	if state.CurrentImageChapter < 0 {
		state.CurrentImageChapter = FirstUnconfirmedChapter(state)
	}
	return state
}

func LatestRunnableStage(state *IllustrationSessionState) string {
	if state == nil || state.Story == nil || len(state.Story.Chapters) == 0 {
		return StageStory
	}
	if len(state.Characters) == 0 {
		return StageCharacter
	}
	if FirstUnconfirmedChapter(state) >= 0 {
		return StageImage
	}
	if strings.TrimSpace(state.SelectedVoiceType) == "" {
		return StageVoiceSelection
	}
	return StageChapterVideo
}

func FirstUnconfirmedChapter(state *IllustrationSessionState) int {
	if state == nil || state.Story == nil || len(state.Story.Chapters) == 0 {
		return -1
	}
	for i := range state.Story.Chapters {
		if state.ConfirmedImages == nil || len(state.ConfirmedImages[i]) == 0 {
			return i
		}
	}
	return -1
}

func normalizeRestartPlan(plan RestartPlan, latest, input string, forceRestart bool) RestartPlan {
	stage := NormalizeRestartStage(plan.Stage)
	if stage == "" || stage == StageCurrent {
		stage = latest
	}
	plan.Stage = stage
	if plan.ChapterIndex < 0 {
		plan.ChapterIndex = -1
	}
	if strings.TrimSpace(plan.Feedback) == "" {
		plan.Feedback = input
	}
	if strings.TrimSpace(plan.Reason) == "" {
		plan.Reason = "模型未给出明确原因，使用默认判断"
	}
	plan.ShouldRestart = forceRestart || plan.ShouldRestart
	return plan
}

func inferRestartPlanHeuristic(input, latest string) RestartPlan {
	lower := strings.ToLower(strings.TrimSpace(input))
	plan := RestartPlan{
		ShouldRestart: looksLikeRestart(lower),
		Stage:         StageCurrent,
		ChapterIndex:  extractChapterIndex(lower),
		Feedback:      input,
		Reason:        "fallback heuristic",
	}
	if strings.Contains(lower, "当前") || strings.Contains(lower, "最新") || strings.Contains(lower, "继续") {
		plan.Stage = latest
		return plan
	}
	if strings.Contains(lower, "故事") || strings.Contains(lower, "主题") || strings.Contains(lower, "章节内容") {
		plan.Stage = StageStory
		plan.ShouldRestart = true
		return plan
	}
	if strings.Contains(lower, "角色") || strings.Contains(lower, "形象") {
		plan.Stage = StageCharacter
		plan.ShouldRestart = true
		return plan
	}
	if strings.Contains(lower, "首帧") || strings.Contains(lower, "图片") || strings.Contains(lower, "画面") || strings.Contains(lower, "插画") {
		plan.Stage = StageImage
		plan.ShouldRestart = true
		return plan
	}
	if strings.Contains(lower, "音色") || strings.Contains(lower, "配音") || strings.Contains(lower, "声音") {
		plan.Stage = StageVoiceSelection
		plan.ShouldRestart = true
		return plan
	}
	if strings.Contains(lower, "视频") || strings.Contains(lower, "解说") || strings.Contains(lower, "合成") {
		plan.Stage = StageChapterVideo
		plan.ShouldRestart = true
		return plan
	}
	plan.Stage = latest
	return plan
}

func looksLikeRestart(input string) bool {
	keywords := []string{"回到", "从", "重新", "重跑", "重做", "重新执行", "重新生成", "跳到", "开始跑", "恢复执行"}
	for _, keyword := range keywords {
		if strings.Contains(input, keyword) {
			return true
		}
	}
	return false
}

func extractChapterIndex(input string) int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`第\s*([0-9]+)\s*章`),
		regexp.MustCompile(`chapter\s*([0-9]+)`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(input); len(match) == 2 {
			n, _ := strconv.Atoi(match[1])
			if n > 0 {
				return n - 1
			}
		}
	}
	chinese := regexp.MustCompile(`第\s*([一二三四五六七八九十])\s*章`).FindStringSubmatch(input)
	if len(chinese) == 2 {
		if n := chineseNumber(chinese[1]); n > 0 {
			return n - 1
		}
	}
	return -1
}

func chineseNumber(s string) int {
	switch s {
	case "一":
		return 1
	case "二":
		return 2
	case "三":
		return 3
	case "四":
		return 4
	case "五":
		return 5
	case "六":
		return 6
	case "七":
		return 7
	case "八":
		return 8
	case "九":
		return 9
	case "十":
		return 10
	default:
		return 0
	}
}

func summarizeRestartState(state *IllustrationSessionState) string {
	if state == nil {
		return "empty state"
	}
	chapterCount := 0
	theme := ""
	if state.Story != nil {
		chapterCount = len(state.Story.Chapters)
		theme = strings.TrimSpace(state.Story.Theme)
	}
	return fmt.Sprintf("state=%s theme=%s chapters=%d characters=%d image_prompts=%d confirmed_images=%d selected_voice=%s chapter_video_prompts=%d final_video=%t",
		state.State,
		theme,
		chapterCount,
		len(state.Characters),
		len(state.ImagePrompts),
		len(state.ConfirmedImages),
		state.SelectedVoiceName,
		len(state.ChapterVideoPrompts),
		strings.TrimSpace(state.VideoURL) != "",
	)
}

func cloneStory(story *model.Story) *model.Story {
	if story == nil {
		return nil
	}
	clone := *story
	clone.Chapters = append([]model.StoryChapter(nil), story.Chapters...)
	return &clone
}

func cloneCharacterProfiles(characters []model.CharacterProfile) []model.CharacterProfile {
	if characters == nil {
		return nil
	}
	out := make([]model.CharacterProfile, len(characters))
	for i, character := range characters {
		out[i] = character
		out[i].Aliases = append([]string(nil), character.Aliases...)
		out[i].ChapterIndices = append([]int(nil), character.ChapterIndices...)
		out[i].ReferenceImageURLs = append([]string(nil), character.ReferenceImageURLs...)
	}
	return out
}

func cloneStringSliceMap(src map[int][]string) map[int][]string {
	if src == nil {
		return nil
	}
	out := make(map[int][]string, len(src))
	for k, v := range src {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func cloneStringMap(src map[int]string) map[int]string {
	if src == nil {
		return nil
	}
	out := make(map[int]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
