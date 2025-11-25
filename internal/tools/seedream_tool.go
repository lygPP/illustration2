package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"illustration2/internal/volc"
)

type SeedreamTool struct {
	ark   *volc.ArkClient
	Model string
}

type SeedreamArgs struct {
	Model  string      `json:"model"`
	Prompt string      `json:"prompt"`
	Image  interface{} `json:"image"`
	Images []string    `json:"images"`
	Size   string      `json:"size"`
	Seq    string      `json:"sequential_image_generation"`
}

type SeedreamResp struct {
	Images []string `json:"images"`
	Count  int      `json:"count"`
}

func NewSeedreamTool(ark *volc.ArkClient) *SeedreamTool {
	return &SeedreamTool{ark: ark, Model: "ep-20251124201143-rwjnq"}
}

func (t *SeedreamTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	params := map[string]*schema.ParameterInfo{
		"prompt":                      {Type: schema.String, Required: true, Desc: "图片提示词"},
		"size":                        {Type: schema.String, Required: false, Desc: "输出分辨率，如1024x1024"},
		"sequential_image_generation": {Type: schema.String, Required: false, Desc: "auto或disabled"},
	}
	return &schema.ToolInfo{
		Name:        "image_generate",
		Desc:        "调用Seedream 4.0生成单图或组图，支持参考图片",
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

func (t *SeedreamTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var args SeedreamArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}
	if args.Prompt == "" {
		return "", errors.New("prompt required")
	}
	var inputs []string
	switch v := args.Image.(type) {
	case string:
		if v != "" {
			inputs = append(inputs, v)
		}
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok && s != "" {
				inputs = append(inputs, s)
			}
		}
	}
	if len(args.Images) > 0 {
		inputs = append(inputs, args.Images...)
	}
	res, err := t.ark.GenerateImages(ctx, volc.ImageGenParams{
		Model:                     t.Model,
		Prompt:                    args.Prompt,
		Size:                      args.Size,
		SequentialImageGeneration: args.Seq,
	})
	if err != nil {
		return "", err
	}
	out := SeedreamResp{Images: res, Count: len(res)}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SeedanceTool 实现eino框架的视频生成工具
type SeedanceTool struct {
	ark   *volc.ArkClient
	Model string
}

// SeedanceArgs 视频生成请求参数
type SeedanceArgs struct {
	Model              string   `json:"model"`
	Prompt             string   `json:"prompt"`
	ReferenceImageURLs []string `json:"reference_image_urls"`
	FirstFrameURL      string   `json:"first_frame_url"`
	LastFrameURL       string   `json:"last_frame_url"`
}

// SeedanceResp 视频生成响应
type SeedanceResp struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	VideoURL string `json:"video_url"`
}

// NewSeedanceTool 创建视频生成工具实例
func NewSeedanceTool(ark *volc.ArkClient) *SeedanceTool {
	return &SeedanceTool{ark: ark, Model: "ep-20251124201423-clr5b"}
}

// Info 获取视频生成工具信息
func (t *SeedanceTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	params := map[string]*schema.ParameterInfo{
		"prompt":               {Type: schema.String, Required: true, Desc: "视频描述提示词"},
		"reference_image_urls": {Type: schema.Array, Required: false, Desc: "参考图片URL列表", ElemInfo: &schema.ParameterInfo{Type: schema.String}},
		"first_frame_url":      {Type: schema.String, Required: false, Desc: "首帧图片URL"},
		"last_frame_url":       {Type: schema.String, Required: false, Desc: "尾帧图片URL"},
	}
	return &schema.ToolInfo{
		Name:        "video_generate",
		Desc:        "调用Seedance生成视频，支持文生视频、图生视频等多种模式",
		ParamsOneOf: schema.NewParamsOneOfByParams(params),
	}, nil
}

// InvokableRun 执行视频生成任务
func (t *SeedanceTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var args SeedanceArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", err
	}

	if args.Prompt == "" {
		return "", errors.New("prompt required")
	}

	// 调用ark客户端创建视频生成任务
	taskID, err := t.ark.CreateVideoTask(ctx, volc.VideoTaskParams{
		Model:              t.Model,
		Prompt:             args.Prompt,
		ReferenceImageURLs: args.ReferenceImageURLs,
		FirstFrameURL:      args.FirstFrameURL,
		LastFrameURL:       args.LastFrameURL,
	})
	if err != nil {
		return "", err
	}

	// 轮训获取视频生成结果
	for {
		status, url, err := t.ark.GetVideoTask(ctx, taskID)
		if err != nil {
			return "", err
		}
		if status == "success" {
			out := SeedanceResp{TaskID: taskID, Status: status, VideoURL: url}
			b, err := json.Marshal(out)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
		time.Sleep(5 * time.Second)
	}
}

var _ einotool.InvokableTool = (*SeedreamTool)(nil)
var _ einotool.InvokableTool = (*SeedanceTool)(nil)
