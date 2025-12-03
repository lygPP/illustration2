package ill_agent

import (
	"context"
	"fmt"
	"illustration2/internal/model"
	"log"
	"sync"

	"github.com/cloudwego/eino/adk"

	adkModel "github.com/cloudwego/eino-examples/adk/common/model"
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

func NewStoryAgent() adk.Agent {
	ctx := context.Background()

	a, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "StoryAgent",
		Description: "An agent that can write poems",
		Instruction: `You are an expert writer that can write poems. 
If feedback is received for the previous version of your poem, you need to modify the poem according to the feedback.
Your response should ALWAYS contain ONLY the poem, and nothing else.`,
		Model:     adkModel.NewChatModel(),
		OutputKey: "content_to_review",
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create chatmodel: %w", err))
	}

	la, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:        "Writer MultiAgent",
		Description: "An agent that can write poems",
		SubAgents: []adk.Agent{a,
			&StoryReviewAgent{AgentName: "ReviewerAgent", AgentDesc: "An agent that can review poems"}},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create loopagent: %w", err))
	}

	return la
}
