package agent

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"

	"illustration2/internal/model"
	"illustration2/internal/volc"
)

// SessionState 会话状态
type SessionState struct {
	State           string              `json:"state"`                      // 当前状态
	Story           *model.Story        `json:"story,omitempty"`            // 生成的故事
	ConfirmedStory  *model.Story        `json:"confirmed_story,omitempty"`  // 用户确认的故事
	ImagePrompts    []model.ImagePrompt `json:"image_prompts,omitempty"`    // 图片生成提示词
	GeneratedImages map[int][]string    `json:"generated_images,omitempty"` // 生成的图片，key为章节索引
	ConfirmedImages map[int][]string    `json:"confirmed_images,omitempty"` // 用户确认的图片，key为章节索引
	VideoURL        string              `json:"video_url,omitempty"`        // 最终生成的视频URL
}

// ChildIllustrationAgent 儿童插画视频生成助手agent
type ChildIllustrationAgent struct {
	chatModel *ark.ChatModel
	arkClient *volc.ArkClient
	storyTool tool.InvokableTool
	imageTool tool.InvokableTool
	videoTool tool.InvokableTool
	sessions  map[string]*SessionState // 会话状态管理
	sessionMu sync.RWMutex
}

// NewChildIllustrationAgent 创建新的儿童插画视频生成助手实例
func NewChildIllustrationAgent(arkClient *volc.ArkClient, storyTool, imageTool, videoTool tool.InvokableTool) *ChildIllustrationAgent {
	apiKey := os.Getenv("ARK_API_KEY")
	chatModel, _ := ark.NewChatModel(context.Background(), &ark.ChatModelConfig{
		APIKey:     apiKey,
		Region:     "cn-beijing",
		HTTPClient: &http.Client{},
		Model:      "ep-20250220181854-c8s82",
	})

	return &ChildIllustrationAgent{
		chatModel: chatModel,
		storyTool: storyTool,
		imageTool: imageTool,
		videoTool: videoTool,
		sessions:  make(map[string]*SessionState),
	}
}

// GetSessionState 获取会话状态
func (a *ChildIllustrationAgent) GetSessionState(sessionID string) *SessionState {
	a.sessionMu.RLock()
	defer a.sessionMu.RUnlock()

	state, exists := a.sessions[sessionID]
	if !exists {
		// 创建新的会话状态
		state = &SessionState{
			State:           "init",
			GeneratedImages: make(map[int][]string),
			ConfirmedImages: make(map[int][]string),
		}
	}

	return state
}

// SaveSessionState 保存会话状态
func (a *ChildIllustrationAgent) SaveSessionState(sessionID string, state *SessionState) {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()

	a.sessions[sessionID] = state
}

func (a *ChildIllustrationAgent) getToolInfos(ctx context.Context) []*schema.ToolInfo {
	res := make([]*schema.ToolInfo, 0)
	tmpInfo, _ := a.storyTool.Info(ctx)
	res = append(res, tmpInfo)
	tmpInfo, _ = a.imageTool.Info(ctx)
	res = append(res, tmpInfo)
	tmpInfo, _ = a.videoTool.Info(ctx)
	res = append(res, tmpInfo)
	return res
}

func (a *ChildIllustrationAgent) getToolNode(ctx context.Context) *compose.ToolsNode {
	tools := []tool.BaseTool{a.storyTool, a.imageTool, a.videoTool}
	toolsNode, err := compose.NewToolNode(ctx, &compose.ToolsNodeConfig{
		Tools: tools,
	})
	if err != nil {
		fmt.Printf("Error creating tools node: %v\n", err)
	}
	return toolsNode
}

func (a *ChildIllustrationAgent) ChatWithGraph(ctx context.Context, input map[string]any) (map[string]any, error) {
	template := prompt.FromMessages(schema.FString,
		schema.SystemMessage("你是一个儿童插画视频生成助手，负责根据用户的主题创作插画视频。"),
		&schema.Message{
			Role:    schema.User,
			Content: "用户提问的主题是：{question}。",
		})
	messages, _ := template.Format(ctx, input)

	toolCallChatModel, _ := a.chatModel.WithTools(a.getToolInfos(ctx))

	graph := compose.NewGraph[[]*schema.Message, *schema.Message]()
	// 添加模型节点
	graph.AddChatModelNode("chat1", toolCallChatModel, compose.WithNodeName("LLM"))
	// 添加lambda节点，记录模型结果到上下文
	graph.AddLambdaNode("record_history", compose.InvokableLambda(func(ctx context.Context, input *schema.Message) (*schema.Message, error) {
		messages = append(messages, input)
		return input, nil
	}), compose.WithNodeName("RecordHistory"))
	// 添加工具节点
	graph.AddToolsNode("tools", a.getToolNode(ctx), compose.WithNodeName("Tools"))
	// 添加lambda节点，记录工具执行结果到上下文
	graph.AddLambdaNode("merge", compose.InvokableLambda(func(ctx context.Context, input []*schema.Message) ([]*schema.Message, error) {
		messages = append(messages, input...)
		return messages, nil
	}), compose.WithNodeName("Merge"))

	// 连接节点：构建graph
	graph.AddEdge(compose.START, "chat1")
	graph.AddEdge("chat1", "record_history")
	// 分支逻辑
	graph.AddBranch("record_history", compose.NewGraphBranch[*schema.Message](func(ctx context.Context, input *schema.Message) (node string, err error) {
		if len(input.ToolCalls) > 0 {
			return "tools", nil
		}
		return compose.END, nil
	}, map[string]bool{
		"tools":     true,
		compose.END: true,
	}))
	graph.AddEdge("tools", "merge")
	graph.AddEdge("merge", "chat1")
	// 编译执行
	gagent, err := graph.Compile(ctx, compose.WithMaxRunSteps(100))
	if err != nil {
		return nil, err
	}
	// 执行graph
	output, err := gagent.Invoke(ctx, messages)
	if err != nil {
		return nil, err
	}
	fmt.Printf("output: %s\n", output.String())

	return nil, nil
}
