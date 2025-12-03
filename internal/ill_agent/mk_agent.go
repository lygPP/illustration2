package ill_agent

import (
	"context"
	"illustration2/internal/model"
	"sync"
)

// SessionState 会话状态
type IllustrationSessionState struct {
	State           string              `json:"state"`                        // 当前状态
	Story           *model.Story        `json:"story,omitempty"`              // 生成的故事
	ImagePrompts    []model.ImagePrompt `json:"image_prompts,omitempty"`      // 图片生成提示词
	GeneratedImages map[int][]string    `json:"generated_images,omitempty"`   // 生成的图片，key为章节索引
	VideoURL        string              `json:"video_url,omitempty"`          // 最终生成的视频URL
	NeedToEditStory bool                `json:"need_to_edit_story,omitempty"` // 是否需要编辑故事
	StoryFeedback   string              `json:"story_feedback,omitempty"`     // 故事反馈
	NeedToEditImage bool                `json:"need_to_edit_image,omitempty"` // 是否需要编辑图片
	ImageFeedback   string              `json:"image_feedback,omitempty"`     // 图片反馈
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
			State:           "init",
			Story:           &model.Story{},
			ImagePrompts:    []model.ImagePrompt{},
			GeneratedImages: make(map[int][]string),
		}
	}

	return state
}

func SaveSessionState(ctx context.Context, state *IllustrationSessionState) {
	sessionMu.Lock()
	defer sessionMu.Unlock()

	sessions[GetSessionID(ctx)] = state
}
