package ill_agent

import (
	"context"
	"fmt"
	"illustration2/internal/auth"
	"illustration2/internal/model"
	"illustration2/internal/usage"
	"log"
	"sync"

	"github.com/cloudwego/eino/adk"
)

// SessionState 会话状态
type IllustrationSessionState struct {
	State                    string                   `json:"state"`                           // 当前状态
	Story                    *model.Story             `json:"story,omitempty"`                 // 生成的故事
	Characters               []model.CharacterProfile `json:"characters,omitempty"`            // 全局角色设定和参考图
	ImagePrompts             []model.ImagePrompt      `json:"image_prompts,omitempty"`         // 图片生成提示词
	GeneratedImages          map[int][]string         `json:"generated_images,omitempty"`      // 生成的图片，key为章节索引
	ConfirmedImages          map[int][]string         `json:"confirmed_images,omitempty"`      // 用户确认的首帧图，key为章节索引
	CurrentImageChapter      int                      `json:"current_image_chapter,omitempty"` // 当前正在生成/审核首帧图的章节索引
	VideoPrompt              string                   `json:"video_prompt,omitempty"`          // 视频生成提示词
	ChapterVideoPrompts      []model.VideoPrompt      `json:"chapter_video_prompts,omitempty"` // 视频生成提示词
	ChapterVideoURLs         map[int]string           `json:"chapter_video_urls,omitempty"`
	ChapterAudioURLs         map[int]string           `json:"chapter_audio_urls,omitempty"`
	NarratedChapterVideoURLs map[int]string           `json:"narrated_chapter_video_urls,omitempty"`
	VideoURL                 string                   `json:"video_url,omitempty"` // 最终生成的视频URL
	SelectedVoiceID          string                   `json:"selected_voice_id,omitempty"`
	SelectedVoiceType        string                   `json:"selected_voice_type,omitempty"`
	SelectedVoiceName        string                   `json:"selected_voice_name,omitempty"`
	NeedToEditStory          bool                     `json:"need_to_edit_story,omitempty"`      // 是否需要编辑故事
	StoryFeedback            string                   `json:"story_feedback,omitempty"`          // 故事反馈
	NeedToEditCharacters     bool                     `json:"need_to_edit_characters,omitempty"` // 是否需要编辑角色
	CharacterFeedback        string                   `json:"character_feedback,omitempty"`      // 角色反馈
	NeedToEditImage          bool                     `json:"need_to_edit_image,omitempty"`      // 是否需要编辑图片
	ImageFeedback            string                   `json:"image_feedback,omitempty"`          // 图片反馈
	NeedToEditImages         bool                     `json:"need_to_edit_images,omitempty"`     // 是否需要编辑图片
	StartFromStage           string                   `json:"start_from_stage,omitempty"`        // 历史恢复/重新执行时的起点阶段
	StartFromChapter         int                      `json:"start_from_chapter,omitempty"`      // 图片阶段从第几章开始，-1 表示自动判断
	RestartFeedback          string                   `json:"restart_feedback,omitempty"`        // 触发重新执行的人工反馈
	RestartReason            string                   `json:"restart_reason,omitempty"`          // 起点判断原因
}

var sessions map[string]*IllustrationSessionState = make(map[string]*IllustrationSessionState) // 会话状态管理
var sessionMu sync.RWMutex

func GetSessionID(ctx context.Context) string {
	sessionID, ok := ctx.Value("sessionID").(string)
	if !ok {
		return "unknow"
	}
	return sessionID
}

func GetSessionState(ctx context.Context) *IllustrationSessionState {
	sessionMu.RLock()
	defer sessionMu.RUnlock()

	state, exists := sessions[GetSessionID(ctx)]
	if !exists {
		// 创建新的会话状态
		state = &IllustrationSessionState{
			State:                    "init",
			Story:                    &model.Story{},
			Characters:               []model.CharacterProfile{},
			ImagePrompts:             []model.ImagePrompt{},
			GeneratedImages:          make(map[int][]string),
			ConfirmedImages:          make(map[int][]string),
			ChapterVideoPrompts:      []model.VideoPrompt{},
			ChapterVideoURLs:         make(map[int]string),
			ChapterAudioURLs:         make(map[int]string),
			NarratedChapterVideoURLs: make(map[int]string),
			StartFromChapter:         -1,
		}
	}

	return state
}

func SaveSessionState(ctx context.Context, state *IllustrationSessionState) {
	sessionMu.Lock()
	sessions[GetSessionID(ctx)] = state
	sessionMu.Unlock()

	SyncAgentWork(ctx, "processing", "")
}

func SyncAgentWork(ctx context.Context, status, errorMessage string) {
	store := GetAgentStore(ctx)
	userID := GetAgentUserID(ctx)
	sessionID := GetSessionID(ctx)
	if store == nil || userID == "" || sessionID == "" || sessionID == "unknow" {
		return
	}
	sessionMu.RLock()
	state, exists := sessions[sessionID]
	sessionMu.RUnlock()
	if !exists || state == nil {
		return
	}
	_ = store.UpdateAgentWorkSnapshot(userID, sessionID, status, errorMessage, auth.AgentWorkSnapshot{
		State:                    state.State,
		Story:                    state.Story,
		Characters:               state.Characters,
		ImagePrompts:             state.ImagePrompts,
		GeneratedImages:          state.GeneratedImages,
		ConfirmedImages:          state.ConfirmedImages,
		CurrentImageChapter:      state.CurrentImageChapter,
		VideoPrompt:              state.VideoPrompt,
		ChapterVideoPrompts:      state.ChapterVideoPrompts,
		ChapterVideoURLs:         state.ChapterVideoURLs,
		ChapterAudioURLs:         state.ChapterAudioURLs,
		NarratedChapterVideoURLs: state.NarratedChapterVideoURLs,
		VideoURL:                 state.VideoURL,
		SelectedVoiceID:          state.SelectedVoiceID,
		SelectedVoiceType:        state.SelectedVoiceType,
		SelectedVoiceName:        state.SelectedVoiceName,
	})
}

type agentContextKey string

const (
	agentUserIDKey agentContextKey = "agentUserID"
	agentStoreKey  agentContextKey = "agentStore"
)

func WithAgentContext(ctx context.Context, sessionID, userID string, store *auth.Store) context.Context {
	ctx = context.WithValue(ctx, "sessionID", sessionID)
	ctx = context.WithValue(ctx, agentUserIDKey, userID)
	ctx = context.WithValue(ctx, agentStoreKey, store)
	ctx = usage.WithContext(ctx, userID, store)
	return ctx
}

func GetAgentUserID(ctx context.Context) string {
	userID, _ := ctx.Value(agentUserIDKey).(string)
	return userID
}

func GetAgentStore(ctx context.Context) *auth.Store {
	store, _ := ctx.Value(agentStoreKey).(*auth.Store)
	return store
}

func NewMKAgent(ctx context.Context) adk.Agent {
	la, err := adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
		Name:        "插画Agent",
		Description: "一个可以生成儿童插画的Agent",
		SubAgents: []adk.Agent{
			NewStoryAgent(ctx),
			NewCharacterAgent(ctx),
			NewImageAgent(ctx),
			NewVoiceSelectionAgent(ctx),
			NewChapterVideoAgent(ctx),
		},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create sequentialagent: %w", err))
	}

	return la
}
