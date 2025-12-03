package tools

import (
	"context"
	"encoding/json"
	"errors"
	"illustration2/internal/volc"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// 实现eino框架的视频生成工具
type VideoTool struct {
	ark   *volc.ArkClient
	Model string
}

// 视频生成请求参数
type VideoToolArgs struct {
	Model              string   `json:"model"`
	Prompt             string   `json:"prompt"`
	ReferenceImageURLs []string `json:"reference_image_urls"`
	FirstFrameURL      string   `json:"first_frame_url"`
	LastFrameURL       string   `json:"last_frame_url"`
}

// 视频生成响应
type VideoToolResp struct {
	TaskID   string `json:"task_id"`
	Status   string `json:"status"`
	VideoURL string `json:"video_url"`
}

// NewVideoTool 创建视频生成工具实例
func NewVideoTool(ark *volc.ArkClient) *VideoTool {
	return &VideoTool{ark: ark, Model: "ep-20251124201423-clr5b"}
}

// Info 获取视频生成工具信息
func (t *VideoTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
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
func (t *VideoTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var args VideoToolArgs
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
			out := VideoToolResp{TaskID: taskID, Status: status, VideoURL: url}
			b, err := json.Marshal(out)
			if err != nil {
				return "", err
			}
			return string(b), nil
		}
		time.Sleep(5 * time.Second)
	}
}

var _ einotool.InvokableTool = (*VideoTool)(nil)
