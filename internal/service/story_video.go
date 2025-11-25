package service

import (
	"context"
	"errors"
	"fmt"
	stdos "os"
	"strings"
	"time"

	"illustration2/internal/volc"
)

type Chapter struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

type Result struct {
	VideoURL string `json:"video_url"`
}

type StoryVideoService struct {
	ark *volc.ArkClient
}

func NewStoryVideoService(ark *volc.ArkClient) *StoryVideoService {
	return &StoryVideoService{ark: ark}
}

func (s *StoryVideoService) Run(ctx context.Context, theme string, chapters int) (*Result, error) {
	if strings.TrimSpace(theme) == "" {
		return nil, errors.New("empty theme")
	}

	story, err := s.generateStory(ctx, theme, chapters)
	if err != nil {
		return nil, fmt.Errorf("generate story: %w", err)
	}

	var allImageURLs []string
	for _, ch := range story {
		imgs, err := s.ark.GenerateImages(ctx, volc.ImageGenParams{
			Model:                     "doubao-seedream-4.0",
			Prompt:                    ch.Title + "。" + ch.Content,
			Size:                      "1024x1024",
			SequentialImageGeneration: "auto",
		})
		if err != nil {
			return nil, fmt.Errorf("generate images: %w", err)
		}
		allImageURLs = append(allImageURLs, imgs...)
	}

	vidTaskID, err := s.ark.CreateVideoTask(ctx, volc.VideoTaskParams{
		Model:              "doubao-seedance-1-0-lite-i2v",
		Prompt:             buildVideoPrompt(story),
		ReferenceImageURLs: allImageURLs,
	})
	if err != nil {
		return nil, fmt.Errorf("create video task: %w", err)
	}

	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		status, url, err := s.ark.GetVideoTask(ctx, vidTaskID)
		if err != nil {
			return nil, fmt.Errorf("get video task: %w", err)
		}
		switch status {
		case "succeeded", "success", "completed":
			if url != "" {
				return &Result{VideoURL: url}, nil
			}
			return nil, errors.New("video succeeded but url empty")
		case "failed", "error":
			return nil, errors.New("video generation failed")
		default:
			time.Sleep(3 * time.Second)
		}
	}
	return nil, errors.New("video generation timeout")
}

func (s *StoryVideoService) generateStory(ctx context.Context, theme string, chapters int) ([]Chapter, error) {
	if chapters <= 0 {
		chapters = 3
	}
	if model := stdos.Getenv("ARK_CHAT_MODEL"); model != "" {
		var res []Chapter
		prompt := fmt.Sprintf("请根据主题“%s”生成%d章的故事，以JSON数组返回，每个元素包含title和content字段。示例：[{'title':'第1章...','content':'...'}]。仅返回JSON。", theme, chapters)
		if err := s.ark.ChatJSON(ctx, model, prompt, &res); err == nil && len(res) > 0 {
			return res, nil
		}
	}
	out := make([]Chapter, 0, chapters)
	for i := 1; i <= chapters; i++ {
		out = append(out, Chapter{
			Title:   fmt.Sprintf("第%d章：%s", i, theme),
			Content: fmt.Sprintf("围绕主题“%s”的故事发展到第%d章，人物关系推进，情节逐步升级，并为后续章节埋下伏笔。", theme, i),
		})
	}
	return out, nil
}

func buildVideoPrompt(chs []Chapter) string {
	var b strings.Builder
	b.WriteString("故事概要：")
	for _, ch := range chs {
		b.WriteString(ch.Title)
		b.WriteString("。")
		b.WriteString(ch.Content)
		b.WriteString("\n")
	}
	b.WriteString("--[parameters] style=cinematic, duration=short")
	return b.String()
}
