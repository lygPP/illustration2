package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/components/tool"
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
	ark       *volc.ArkClient
	imageTool tool.InvokableTool
	videoTool tool.InvokableTool
	sessions  map[string]*SessionState // 会话状态管理
	sessionMu sync.RWMutex
}

// NewChildIllustrationAgent 创建新的儿童插画视频生成助手实例
func NewChildIllustrationAgent(ark *volc.ArkClient, imageTool, videoTool tool.InvokableTool) *ChildIllustrationAgent {
	return &ChildIllustrationAgent{
		ark:       ark,
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

func (a *ChildIllustrationAgent) ChatWithGraph(ctx context.Context, input map[string]any) (map[string]any, error) {
	template := prompt.FromMessages(schema.FString,
		schema.SystemMessage("你是一个儿童插画视频生成助手，负责根据用户的主题创作插画视频。"),
		&schema.Message{
			Role:    schema.User,
			Content: "用户提问的主题是：{question}。",
		})
	messages, _ := template.Format(ctx, input)
	fmt.Println(messages)
	return nil, nil
}

// Execute 执行agent逻辑
func (a *ChildIllustrationAgent) Execute(ctx context.Context, sessionID string, input string) (string, string, error) {
	// 获取当前会话状态
	state := a.GetSessionState(sessionID)

	// 根据当前状态执行相应的逻辑
	var response string
	var err error

	switch state.State {
	case "init":
		// 初始状态，开始生成故事
		state.State = "generate_story"
		response, err = a.generateStory(ctx, state, input)
	case "generate_story":
		// 生成故事状态
		response, err = a.generateStory(ctx, state, input)
	case "confirm_story":
		// 故事确认状态
		response, err = a.confirmStory(ctx, state, input)
	case "generate_image_prompts":
		// 生成图片提示词状态
		response, err = a.generateImagePrompts(ctx, state)
	case "generate_images":
		// 生成图片状态
		response, err = a.generateImages(ctx, state)
	case "confirm_images":
		// 图片确认状态
		response, err = a.confirmImages(ctx, state, input)
	case "generate_video":
		// 生成视频状态
		response, err = a.generateVideo(ctx, state)
	case "complete":
		// 完成状态
		response, err = a.handleComplete(ctx, state)
	default:
		response = "未知状态，请重新开始"
		state.State = "init"
	}

	// 保存状态
	a.SaveSessionState(sessionID, state)

	return response, state.State, err
}

// generateStory 生成故事
func (a *ChildIllustrationAgent) generateStory(ctx context.Context, state *SessionState, theme string) (string, error) {
	if theme == "" {
		return "请提供故事主题", nil
	}

	// 调用LLM生成故事
	prompt := fmt.Sprintf("请为儿童创作一个关于'%s'的插画故事，生成3-5个简短有趣的章节。每个章节包含标题和内容，语言要生动有趣，适合儿童。请以JSON格式返回，包含theme字段和chapters数组，每个chapter包含title和content字段。", theme)

	var story model.Story
	modelName := "ernie-bot"
	if err := a.ark.ChatJSON(ctx, modelName, prompt, &story); err != nil {
		// 如果LLM调用失败，使用默认故事
		story = model.Story{
			Theme:    theme,
			Chapters: generateDefaultStoryChapters(theme),
		}
	}

	// 如果生成的故事不完整，使用默认结构
	if story.Theme == "" {
		story.Theme = theme
	}
	if len(story.Chapters) == 0 {
		story.Chapters = generateDefaultStoryChapters(theme)
	}

	// 保存故事到状态
	state.Story = &story
	state.State = "confirm_story"

	// 返回故事内容给用户确认
	storyText := a.formatStoryForDisplay(story)
	return fmt.Sprintf("故事生成完成，请确认是否满意：\n\n%s\n\n如果满意请回复'确认'或'ok'，不满意请告诉我需要修改的地方。", storyText), nil
}

// confirmStory 确认故事
func (a *ChildIllustrationAgent) confirmStory(ctx context.Context, state *SessionState, input string) (string, error) {
	if state == nil || state.Story == nil {
		return "故事信息丢失，请重新开始", nil
	}

	inputLower := strings.ToLower(input)
	if strings.Contains(inputLower, "确认") || strings.Contains(inputLower, "ok") || strings.Contains(inputLower, "好的") {
		// 用户确认故事，保存到confirmedStory
		state.ConfirmedStory = state.Story
		state.State = "generate_image_prompts"
		return "故事已确认，正在生成图片提示词...", nil
	}

	// 用户不确认，需要重新生成故事，使用用户的反馈进行调整
	state.State = "generate_story"
	return fmt.Sprintf("收到您的反馈，我将根据'%s'重新生成故事。", input), nil
}

// generateImagePrompts 生成图片提示词
func (a *ChildIllustrationAgent) generateImagePrompts(ctx context.Context, state *SessionState) (string, error) {
	if state == nil || state.ConfirmedStory == nil {
		return "故事信息丢失，请重新开始", nil
	}

	// 为每个章节生成图片提示词
	prompts := make([]model.ImagePrompt, 0, len(state.ConfirmedStory.Chapters))
	modelName := "ernie-bot"

	for i, chapter := range state.ConfirmedStory.Chapters {
		prompt := fmt.Sprintf("请为儿童插画故事生成一个详细的图片提示词，用于生成插画。故事章节标题：'%s'，内容：'%s'。提示词需要包含场景、角色、色彩和风格描述，风格要统一为明亮、可爱、卡通的儿童插画风格，适合4-8岁儿童欣赏。", chapter.Title, chapter.Content)

		var response struct {
			Prompt string `json:"prompt"`
		}
		if err := a.ark.ChatJSON(ctx, modelName, prompt, &response); err != nil {
			// 如果LLM调用失败，使用默认提示词
			response.Prompt = fmt.Sprintf("明亮可爱的儿童插画，%s，卡通风格，丰富色彩，适合儿童，清晰细节", chapter.Title)
		}

		prompts = append(prompts, model.ImagePrompt{
			ChapterIndex: i,
			Prompt:       response.Prompt,
		})
	}

	state.ImagePrompts = prompts
	state.State = "generate_images"

	return "图片提示词生成完成，正在生成插画...", nil
}

// generateImages 生成图片
func (a *ChildIllustrationAgent) generateImages(ctx context.Context, state *SessionState) (string, error) {
	if state == nil || state.ConfirmedStory == nil || len(state.ImagePrompts) == 0 {
		return "信息不完整，请重新开始", nil
	}

	// 初始化生成的图片map
	if state.GeneratedImages == nil {
		state.GeneratedImages = make(map[int][]string)
	}

	// 为每个章节生成图片
	for _, prompt := range state.ImagePrompts {
		// 调用图片生成工具
		args := map[string]interface{}{
			"prompt":                      prompt.Prompt,
			"size":                        "1024x1024",
			"sequential_image_generation": "auto",
		}

		argsJSON, _ := json.Marshal(args)
		result, err := a.imageTool.InvokableRun(ctx, string(argsJSON))
		if err != nil {
			return fmt.Sprintf("生成图片失败: %v", err), nil
		}

		// 解析图片生成结果
		var imageResp struct {
			Images []string `json:"images"`
		}
		if err := json.Unmarshal([]byte(result), &imageResp); err != nil {
			return fmt.Sprintf("解析图片结果失败: %v", err), nil
		}

		state.GeneratedImages[prompt.ChapterIndex] = imageResp.Images
	}

	state.State = "confirm_images"

	// 返回图片生成结果给用户确认
	chapterCount := len(state.GeneratedImages)
	return fmt.Sprintf("插画生成完成，共生成了%d个章节的插画。请确认是否满意，如果满意请回复'确认'或'ok'，不满意请告诉我需要修改的地方。", chapterCount), nil
}

// confirmImages 确认图片
func (a *ChildIllustrationAgent) confirmImages(ctx context.Context, state *SessionState, input string) (string, error) {
	if state == nil || len(state.GeneratedImages) == 0 {
		return "图片信息丢失，请重新开始", nil
	}

	inputLower := strings.ToLower(input)
	if strings.Contains(inputLower, "确认") || strings.Contains(inputLower, "ok") || strings.Contains(inputLower, "好的") {
		// 用户确认图片，保存到confirmedImages
		if state.ConfirmedImages == nil {
			state.ConfirmedImages = make(map[int][]string)
		}
		for k, v := range state.GeneratedImages {
			state.ConfirmedImages[k] = v
		}
		state.State = "generate_video"
		return "图片已确认，正在生成视频...", nil
	}

	// 用户不确认，需要重新生成图片
	state.State = "generate_images"
	return fmt.Sprintf("收到您的反馈，我将根据'%s'重新生成插画。", input), nil
}

// generateVideo 生成视频
func (a *ChildIllustrationAgent) generateVideo(ctx context.Context, state *SessionState) (string, error) {
	if state == nil || state.ConfirmedStory == nil || len(state.ConfirmedImages) == 0 {
		return "信息不完整，请重新开始", nil
	}

	// 收集所有确认的图片URL
	var imageURLs []string
	for i := 0; i < len(state.ConfirmedStory.Chapters); i++ {
		if images, ok := state.ConfirmedImages[i]; ok {
			imageURLs = append(imageURLs, images...)
		}
	}

	// 构建视频提示词
	videoPrompt := buildVideoPrompt(state.ConfirmedStory)

	// 调用视频生成工具
	args := map[string]interface{}{
		"prompt":               videoPrompt,
		"reference_image_urls": imageURLs,
	}

	argsJSON, _ := json.Marshal(args)
	result, err := a.videoTool.InvokableRun(ctx, string(argsJSON))
	if err != nil {
		return fmt.Sprintf("生成视频失败: %v", err), nil
	}

	// 解析视频生成结果
	var videoResp struct {
		VideoURL string `json:"video_url"`
	}
	if err := json.Unmarshal([]byte(result), &videoResp); err != nil {
		return fmt.Sprintf("解析视频结果失败: %v", err), nil
	}

	state.VideoURL = videoResp.VideoURL
	state.State = "complete"

	return "视频生成完成！", nil
}

// handleComplete 处理完成状态
func (a *ChildIllustrationAgent) handleComplete(ctx context.Context, state *SessionState) (string, error) {
	if state == nil || state.VideoURL == "" {
		return "视频生成失败，请重新开始", nil
	}

	return fmt.Sprintf("恭喜！儿童插画视频生成完成。视频链接：%s\n\n您可以通过这个链接查看和下载生成的视频。", state.VideoURL), nil
}

// formatStoryForDisplay 格式化故事用于显示
func (a *ChildIllustrationAgent) formatStoryForDisplay(story model.Story) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("主题：%s\n\n", story.Theme))

	for i, chapter := range story.Chapters {
		builder.WriteString(fmt.Sprintf("第%d章：%s\n", i+1, chapter.Title))
		builder.WriteString(fmt.Sprintf("内容：%s\n\n", chapter.Content))
	}

	return builder.String()
}

// generateDefaultStoryChapters 生成默认故事章节
func generateDefaultStoryChapters(theme string) []model.StoryChapter {
	return []model.StoryChapter{
		{
			Title:   fmt.Sprintf("奇妙的%s之旅开始了", theme),
			Content: fmt.Sprintf("在一个阳光明媚的早晨，小朋友们踏上了寻找%s的奇妙旅程。他们充满好奇，带着期待的心情出发了。", theme),
		},
		{
			Title:   fmt.Sprintf("遇见了%s精灵", theme),
			Content: fmt.Sprintf("旅途中，他们遇见了一位友好的%s精灵。精灵告诉他们关于%s的神奇故事，并决定帮助他们一起寻找。", theme, theme),
		},
		{
			Title:   fmt.Sprintf("勇敢面对挑战", theme),
			Content: fmt.Sprintf("在寻找%s的过程中，小朋友们遇到了一些小困难，但他们相互帮助，勇敢地克服了挑战。", theme),
		},
		{
			Title:   fmt.Sprintf("找到%s的惊喜", theme),
			Content: fmt.Sprintf("经过不懈努力，小朋友们终于找到了%s！他们高兴地跳起来，这次旅程让他们学到了很多宝贵的东西。", theme),
		},
	}
}

// buildVideoPrompt 构建视频生成提示词
func buildVideoPrompt(story *model.Story) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("儿童插画视频，主题：%s\n", story.Theme))
	builder.WriteString("故事概要：\n")

	for _, chapter := range story.Chapters {
		builder.WriteString(fmt.Sprintf("- %s：%s\n", chapter.Title, chapter.Content))
	}

	builder.WriteString("\n视频风格：明亮、可爱的卡通儿童插画风格，色彩丰富，适合4-8岁儿童观看。")
	builder.WriteString("\n视频节奏：缓慢平稳，让孩子们能够清晰地看到每一个画面。")
	builder.WriteString("\n画面转换：平滑自然，保持视觉连贯性。")

	return builder.String()
}

// Info 获取agent信息
func (a *ChildIllustrationAgent) Info() map[string]interface{} {
	return map[string]interface{}{
		"name":        "child_illustration_agent",
		"description": "儿童插画视频生成助手，根据用户输入的主题生成故事章节+内容，并生成完整视频。",
		"states": []string{
			"init",
			"generate_story",
			"confirm_story",
			"generate_image_prompts",
			"generate_images",
			"confirm_images",
			"generate_video",
			"complete",
		},
	}
}
