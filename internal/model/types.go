package model

// StoryChapter 故事章节结构
type StoryChapter struct {
	Title   string `json:"title"`   // 章节标题
	Content string `json:"content"` // 章节内容
}

// Story 故事结构
type Story struct {
	Theme    string         `json:"theme"`    // 故事主题
	Chapters []StoryChapter `json:"chapters"` // 故事章节列表
}

// ImagePrompt 图片生成提示词结构
type ImagePrompt struct {
	ChapterIndex int    `json:"chapter_index"` // 对应章节索引
	Prompt       string `json:"prompt"`        // 图片生成提示词
}

// AgentState agent状态结构
type AgentState struct {
	Story           *Story           `json:"story,omitempty"`            // 生成的故事
	ConfirmedStory  *Story           `json:"confirmed_story,omitempty"`  // 用户确认的故事
	ImagePrompts    []ImagePrompt    `json:"image_prompts,omitempty"`    // 图片生成提示词
	GeneratedImages map[int][]string `json:"generated_images,omitempty"` // 生成的图片，key为章节索引
	ConfirmedImages map[int][]string `json:"confirmed_images,omitempty"` // 用户确认的图片，key为章节索引
	VideoURL        string           `json:"video_url,omitempty"`        // 最终生成的视频URL
}
