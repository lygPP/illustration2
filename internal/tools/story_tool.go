package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"illustration2/internal/model"
	"illustration2/internal/volc"
)

// StoryTool 实现eino框架的故事生成工具
type StoryTool struct {
	ark   *volc.ArkClient
	Model string
}

// StoryToolArgs 故事生成请求参数
type StoryToolArgs struct {
	Theme string `json:"theme"` // 故事主题
}

// StoryToolResp 故事生成响应
type StoryToolResp struct {
	Theme    string               `json:"theme"`    // 故事主题
	Chapters []model.StoryChapter `json:"chapters"` // 故事章节列表
	Message  string               `json:"message"`  // 提示信息
}

// NewStoryTool 创建故事生成工具实例
func NewStoryTool(ark *volc.ArkClient) *StoryTool {
	return &StoryTool{ark: ark, Model: "ep-20250220181854-c8s82"}
}

// Info 获取故事生成工具信息
func (t *StoryTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	params := map[string]*schema.ParameterInfo{
		"theme": {Type: schema.String, Required: true, Desc: "故事主题"},
	}
	return &schema.ToolInfo{
		Name:        "story_generate",
		Desc:        "为儿童创作插画故事，生成3-5个简短有趣的章节",
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

// InvokableRun 执行故事生成任务
func (t *StoryTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var args StoryToolArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}

	if args.Theme == "" {
		return "", errors.New("theme required")
	}

	// 调用LLM生成故事
	prompt := fmt.Sprintf("请为儿童创作一个关于'%s'的插画故事，生成3-5个简短有趣的章节。每个章节包含标题和内容，语言要生动有趣，适合儿童。请以JSON格式返回，包含theme字段和chapters数组，每个chapter包含title和content字段。", args.Theme)

	var story model.Story
	if err := t.ark.ChatJSON(ctx, t.Model, prompt, &story); err != nil {
		// 如果LLM调用失败，使用默认故事
		story = model.Story{
			Theme:    args.Theme,
			Chapters: generateDefaultStoryChapters(args.Theme),
		}
	}

	// 如果生成的故事不完整，使用默认结构
	if story.Theme == "" {
		story.Theme = args.Theme
	}
	if len(story.Chapters) == 0 {
		story.Chapters = generateDefaultStoryChapters(args.Theme)
	}

	// 格式化返回结果
	response := StoryToolResp{
		Theme:    story.Theme,
		Chapters: story.Chapters,
		Message:  "故事生成完成",
	}

	b, err := json.Marshal(response)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

// generateDefaultStoryChapters 生成默认故事章节
func generateDefaultStoryChapters(theme string) []model.StoryChapter {
	return []model.StoryChapter{
		{
			Title:   fmt.Sprintf("%s的开始", theme),
			Content: fmt.Sprintf("从前，在一个神奇的世界里，%s开始了它的冒险。它充满好奇地探索着周围的一切。", theme),
		},
		{
			Title:   fmt.Sprintf("%s的朋友", theme),
			Content: fmt.Sprintf("在路上，%s遇到了许多有趣的朋友。它们一起玩耍，一起分享快乐。", theme),
		},
		{
			Title:   fmt.Sprintf("%s的挑战", theme),
			Content: fmt.Sprintf("突然，%s遇到了一个小麻烦。但是通过勇气和智慧，它成功地解决了问题。", theme),
		},
		{
			Title:   fmt.Sprintf("%s的收获", theme),
			Content: fmt.Sprintf("经历了这次冒险，%s学到了很多新东西，变得更加勇敢和聪明了。", theme),
		},
	}
}

// 确保StoryTool实现了einotool.InvokableTool接口
var _ einotool.InvokableTool = (*StoryTool)(nil)
