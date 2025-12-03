package tools

import (
	"context"
	"encoding/json"
	"errors"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"illustration2/internal/volc"
)

type ImageTool struct {
	ark   *volc.ArkClient
	Model string
}

type ImageToolArgs struct {
	Model  string      `json:"model"`
	Prompt string      `json:"prompt"`
	Image  interface{} `json:"image"`
	Images []string    `json:"images"`
	Size   string      `json:"size"`
	Seq    string      `json:"sequential_image_generation"`
}

type ImageToolResp struct {
	Images []string `json:"images"`
	Count  int      `json:"count"`
}

func NewImageTool(ark *volc.ArkClient) *ImageTool {
	return &ImageTool{ark: ark, Model: "ep-20251124201143-rwjnq"}
}

func (t *ImageTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
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

func (t *ImageTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var args ImageToolArgs
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
	out := ImageToolResp{Images: res, Count: len(res)}
	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

var _ einotool.InvokableTool = (*ImageTool)(nil)
