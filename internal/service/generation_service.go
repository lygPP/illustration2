package service

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/volc"
	"strings"
)

type GenerationService struct {
	arkClient *volc.ArkClient
}

func NewGenerationService(arkClient *volc.ArkClient) *GenerationService {
	return &GenerationService{
		arkClient: arkClient,
	}
}

type GenerationRequest struct {
	GenerateResourceType string   `json:"generateResourceType"` // image, video
	ModelName            string   `json:"modelName"`            // seedream, seedance1.0, seedance2.0
	Size                 string   `json:"size"`                 // e.g. 1024x1024
	GenerateRelyType     string   `json:"generateRelyType"`     // 首帧, 首尾帧, 参考图
	ImageList            []string `json:"imageList"`            // URLs or Base64
	Prompt               string   `json:"prompt"`               // Prompt for generation
}

type GenerationResponse struct {
	Type    string   `json:"type"`              // image, video
	Images  []string `json:"images,omitempty"`  // For image generation
	TaskID  string   `json:"task_id,omitempty"` // For video generation
	Message string   `json:"message,omitempty"`
}

type VideoResultResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"` // succeeded, processing, failed
	URL    string `json:"video_url,omitempty"`
}

func (s *GenerationService) Generate(ctx context.Context, req GenerationRequest) (*GenerationResponse, error) {
	switch req.GenerateResourceType {
	case "image":
		return s.generateImage(ctx, req)
	case "video":
		return s.generateVideo(ctx, req)
	default:
		return nil, errors.New("unsupported generateResourceType: " + req.GenerateResourceType)
	}
}

func (s *GenerationService) generateImage(ctx context.Context, req GenerationRequest) (*GenerationResponse, error) {
	model := req.ModelName
	if model == "seedream" {
		model = "ep-20251124201143-rwjnq" // Default mapping
	}

	params := volc.ImageGenParams{
		Model:                     model,
		Prompt:                    req.Prompt,
		Size:                      req.Size,
		SequentialImageGeneration: "auto", // Default to auto if not specified
		ImageInputs:               req.ImageList,
	}

	images, err := s.arkClient.GenerateImages(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("generate images failed: %w", err)
	}

	return &GenerationResponse{
		Type:   "image",
		Images: images,
	}, nil
}

func (s *GenerationService) GetVideoResult(ctx context.Context, taskID string) (*VideoResultResponse, error) {
	status, url, err := s.arkClient.GetVideoTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get video task failed: %w", err)
	}
	return &VideoResultResponse{
		TaskID: taskID,
		Status: status,
		URL:    url,
	}, nil
}

func (s *GenerationService) generateVideo(ctx context.Context, req GenerationRequest) (*GenerationResponse, error) {
	model := req.ModelName
	if model == "seedance1.0" {
		model = "ep-20251124201423-clr5b"
	} else if model == "seedance2.0" {
		model = "ep-20251124201423-clr5b" // Assuming this mapping, adjust if needed
	}

	params := volc.VideoTaskParams{
		Model:  model,
		Prompt: req.Prompt,
	}

	// Separate URLs and Base64s from ImageList
	var urls []string
	var base64s []string
	for _, img := range req.ImageList {
		if strings.HasPrefix(img, "http") {
			urls = append(urls, img)
		} else {
			base64s = append(base64s, img)
		}
	}

	// Handle GenerateRelyType
	switch req.GenerateRelyType {
	case "首帧":
		if len(urls) > 0 {
			params.FirstFrameURL = urls[0]
		} else if len(base64s) > 0 {
			params.FirstFrameBase64 = base64s[0]
		}
	case "首尾帧":
		if len(urls) >= 1 {
			params.FirstFrameURL = urls[0]
		} else if len(base64s) >= 1 {
			params.FirstFrameBase64 = base64s[0]
		}

		if len(urls) >= 2 {
			params.LastFrameURL = urls[1]
		} else if len(base64s) >= 2 {
			// If we have mixed usage (e.g. 1 url, 1 base64), logic gets complex.
			// Assuming consistent input type or simplistic handling here.
			// If urls has 1, and base64s has 1, maybe base64s[0] is the last frame?
			// Let's stick to simple logic: prioritize URLs for both slots if available.
			if len(urls) < 2 && len(base64s) >= 1 {
				params.LastFrameBase64 = base64s[0]
			} else if len(base64s) >= 2 {
				params.LastFrameBase64 = base64s[1]
			}
		} else if len(base64s) >= 2 {
			params.LastFrameBase64 = base64s[1]
		}
	case "参考图":
		params.ReferenceImageURLs = urls
		params.ReferenceImagesBase64 = base64s
	default:
		// If type is not specified but images are provided, default to reference images?
		// Or maybe it's just text-to-video if no images.
		if len(urls) > 0 {
			params.ReferenceImageURLs = urls
		}
		if len(base64s) > 0 {
			params.ReferenceImagesBase64 = base64s
		}
	}

	taskID, err := s.arkClient.CreateVideoTask(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create video task failed: %w", err)
	}

	return &GenerationResponse{
		Type:    "video",
		TaskID:  taskID,
		Message: "Video generation task created successfully. Use GetVideoTask to check status.",
	}, nil
}
