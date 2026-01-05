package volc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultBase = "https://ark.cn-beijing.volces.com"
)

type ArkClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Mock       bool
}

func NewArkClientDefault() *ArkClient {
	apiKey := os.Getenv("ARK_API_KEY")
	return &ArkClient{
		BaseURL:    defaultBase,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Mock:       strings.ToLower(os.Getenv("ARK_MOCK")) == "1" || strings.ToLower(os.Getenv("ARK_MOCK")) == "true",
	}
}

func NewArkClientWithTimeout(timeout time.Duration) *ArkClient {
	apiKey := os.Getenv("ARK_API_KEY")
	return &ArkClient{
		BaseURL:    defaultBase,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: timeout},
		Mock:       strings.ToLower(os.Getenv("ARK_MOCK")) == "1" || strings.ToLower(os.Getenv("ARK_MOCK")) == "true",
	}
}

type ImageGenParams struct {
	Model                     string
	Prompt                    string
	Size                      string
	SequentialImageGeneration string
	ImageInputs               []string
	MaxImages                 int
}

func (c *ArkClient) GenerateImages(ctx context.Context, p ImageGenParams) ([]string, error) {
	if c.Mock {
		// 1x1 PNG pixel base64
		pixel := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR4nGNgYAAAAAMAASsJTYQAAAAASUVORK5CYII="
		return []string{"data:image/png;base64," + pixel}, nil
	}
	if p.Model == "" {
		p.Model = "doubao-seedream-4.0"
	}
	if p.Size == "" {
		p.Size = "1024x1024"
	}
	if p.MaxImages == 0 {
		p.MaxImages = 1
	}
	body := map[string]any{
		"model":  p.Model,
		"prompt": p.Prompt,
		"size":   p.Size,
	}
	if p.SequentialImageGeneration != "" {
		body["sequential_image_generation"] = p.SequentialImageGeneration
		if p.SequentialImageGeneration == "auto" && p.MaxImages > 0 {
			body["sequential_image_generation_options"] = map[string]any{"max_images": p.MaxImages}
		}
	}
	if len(p.ImageInputs) > 0 {
		body["image"] = p.ImageInputs
	}

	var resp struct {
		Data []struct {
			URL    string `json:"url"`
			B64    string `json:"b64_json"`
			Format string `json:"format"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, "/api/v3/images/generations", body, &resp); err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(resp.Data))
	for _, d := range resp.Data {
		if d.URL != "" {
			urls = append(urls, d.URL)
			continue
		}
		if d.B64 != "" {
			fmtType := d.Format
			if fmtType == "" {
				fmtType = "png"
			}
			urls = append(urls, "data:image/"+fmtType+";base64,"+d.B64)
		}
	}
	if len(urls) == 0 {
		return nil, errors.New("no images returned")
	}
	return urls, nil
}

type VideoTaskParams struct {
	Model              string
	Prompt             string
	ReferenceImageURLs []string
	FirstFrameURL      string
	LastFrameURL       string
}

func (c *ArkClient) CreateVideoTask(ctx context.Context, p VideoTaskParams) (string, error) {
	if c.Mock {
		return "mock-task", nil
	}
	if p.Model == "" {
		p.Model = "doubao-seedance-1-0-lite-i2v"
	}
	content := make([]map[string]any, 0, 3) // 1 text + up to 2 images
	content = append(content, map[string]any{"type": "text", "text": p.Prompt})

	// 处理首帧+尾帧模式
	if p.FirstFrameURL != "" && p.LastFrameURL != "" {
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": p.FirstFrameURL, "role": "first_frame"},
		})
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": p.LastFrameURL, "role": "last_frame"},
		})
		// 处理首帧模式
	} else if p.FirstFrameURL != "" {
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": p.FirstFrameURL, "role": "first_frame"},
		})
		// 处理参考图片模式
	} else if len(p.ReferenceImageURLs) > 0 {
		for _, u := range p.ReferenceImageURLs {
			content = append(content, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": u, "role": "reference_image"},
			})
		}
	}

	body := map[string]any{
		"model":   p.Model,
		"content": content,
	}
	var resp map[string]any
	if err := c.postJSON(ctx, "/api/v3/contents/generations/tasks", body, &resp); err != nil {
		return "", err
	}
	if id, ok := resp["task_id"].(string); ok && id != "" {
		return id, nil
	}
	if id, ok := resp["id"].(string); ok && id != "" {
		return id, nil
	}
	return "", errors.New("no task id in response")
}

func (c *ArkClient) GetVideoTask(ctx context.Context, taskID string) (string, string, error) {
	if c.Mock {
		return "succeeded", "https://example.com/mock_video.mp4", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v3/contents/generations/tasks?task_id="+taskID, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", "", fmt.Errorf("http %d", res.StatusCode)
	}
	var resp map[string]any
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return "", "", err
	}
	status := getString(resp, "status")
	url := getString(resp, "video_url")
	if url == "" {
		if out, ok := resp["output"].(map[string]any); ok {
			url = getString(out, "video_url")
		}
	}
	return status, url, nil
}

func (c *ArkClient) postJSON(ctx context.Context, path string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, strings.NewReader(string(b)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	// 打印req信息和header
	fmt.Printf("POST %s\n%s\n%s\n", req.URL.String(), req.Header, string(b))
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// 打印响应体
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", res.StatusCode, string(bodyBytes))
	}
	// 使用保存的bodyBytes进行解码
	return json.Unmarshal(bodyBytes, out)
}

func getString(m map[string]any, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (c *ArkClient) ChatJSON(ctx context.Context, model string, prompt string) (string, error) {
	if model == "" {
		return "", errors.New("model required")
	}
	reqBody := map[string]any{
		"model":    model,
		"messages": []map[string]any{{"role": "user", "content": prompt}},
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := c.postJSON(ctx, "/api/v3/chat/completions", reqBody, &resp); err != nil {
		return "", err
	}
	var content string
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
		if content == "" {
			content = resp.Choices[0].Delta.Content
		}
	}
	if content == "" {
		return "", errors.New("empty chat content")
	}
	return content, nil
}
