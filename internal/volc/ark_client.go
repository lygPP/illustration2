package volc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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
		fmt.Printf("err: %+v\n", err)
		return nil, err
	}
	fmt.Printf("resp: %+v\n", resp)
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
	Model                 string
	Prompt                string
	ReferenceImageURLs    []string
	ReferenceImagesBase64 []string
	FirstFrameURL         string
	FirstFrameBase64      string
	LastFrameURL          string
	LastFrameBase64       string
	GenerateAudio         *bool
	Duration              int
}

type VoiceCloneParams struct {
	Model          string
	Name           string
	Description    string
	SampleAudioURL string
	SpeakerID      string
	Language       int
}

type VoiceCloneResult struct {
	TaskID          string
	VoiceID         string
	VoiceType       string
	PreviewAudioURL string
	Status          string
}

type VoicePreviewParams struct {
	Model     string
	Text      string
	VoiceID   string
	VoiceType string
	Format    string
}

func (c *ArkClient) CreateVideoTask(ctx context.Context, p VideoTaskParams) (string, error) {
	if c.Mock {
		return "mock-task", nil
	}
	if p.Model == "" {
		p.Model = "doubao-seedance-1-0-lite-i2v"
	}
	genAudio := true
	if p.GenerateAudio != nil {
		genAudio = *p.GenerateAudio
	}
	content := make([]map[string]any, 0, 3) // 1 text + up to 2 images
	content = append(content, map[string]any{"type": "text", "text": p.Prompt})

	// Helper to get image URL or Base64
	getImageSrc := func(url, base64 string) string {
		if url != "" {
			return url
		}
		return base64
	}

	firstFrameSrc := getImageSrc(p.FirstFrameURL, p.FirstFrameBase64)
	lastFrameSrc := getImageSrc(p.LastFrameURL, p.LastFrameBase64)

	// 处理首帧+尾帧模式
	if firstFrameSrc != "" && lastFrameSrc != "" {
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": firstFrameSrc},
			"role":      "first_frame",
		})
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": lastFrameSrc},
			"role":      "last_frame",
		})
		// 处理首帧模式
	} else if firstFrameSrc != "" {
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": firstFrameSrc},
			"role":      "first_frame",
		})
		// 处理参考图片模式
	} else if len(p.ReferenceImageURLs) > 0 {
		for _, u := range p.ReferenceImageURLs {
			content = append(content, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": u},
				"role":      "reference_image",
			})
		}
	} else if len(p.ReferenceImagesBase64) > 0 {
		for _, b := range p.ReferenceImagesBase64 {
			content = append(content, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": b},
				"role":      "reference_image",
			})
		}
	}

	body := map[string]any{
		"model":   p.Model,
		"content": content,
	}
	body["generate_audio"] = genAudio
	if p.Duration > 0 {
		body["duration"] = p.Duration
	}
	var resp map[string]any
	if err := c.postJSON(ctx, "/api/v3/contents/generations/tasks", body, &resp); err != nil {
		fmt.Printf("err: %+v\n", err)
		return "", err
	}
	fmt.Printf("resp: %+v\n", resp)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v3/contents/generations/tasks/"+taskID, nil)
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
		fmt.Printf("err: %+v\n", err)
		return "", "", err
	}
	fmt.Printf("resp: %+v\n", resp)
	status := getString(resp, "status")
	var url string
	if content, ok := resp["content"].(map[string]any); ok {
		url = getString(content, "video_url")
	}
	return status, url, nil
}

func (c *ArkClient) CloneVoice(ctx context.Context, p VoiceCloneParams) (VoiceCloneResult, error) {
	if c.Mock {
		id := "mock_voice_" + fmt.Sprint(time.Now().UnixNano())
		return VoiceCloneResult{
			TaskID:          "mock-voice-task",
			VoiceID:         id,
			VoiceType:       id,
			PreviewAudioURL: mockAudioDataURL(),
			Status:          "ready",
		}, nil
	}
	audioBytes, audioFormat, err := readAudioReference(ctx, c.HTTPClient, p.SampleAudioURL)
	if err != nil {
		return VoiceCloneResult{}, err
	}
	language := p.Language
	if language == 0 {
		language = envInt("OPENSPEECH_VOICE_CLONE_LANGUAGE", 0)
	}
	modelType := envInt("OPENSPEECH_VOICE_CLONE_MODEL_TYPE", 4)
	cloneResourceID := voiceCloneResourceID(modelType)

	customSpeakerID := firstNonEmpty(p.SpeakerID, os.Getenv("OPENSPEECH_VOICE_CLONE_CUSTOM_SPEAKER_ID"), os.Getenv("OPENSPEECH_VOICE_CLONE_SPEAKER_ID"))
	if customSpeakerID == "" {
		customSpeakerID = "voice_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	extraParams := map[string]any{}
	if text := strings.TrimSpace(os.Getenv("OPENSPEECH_VOICE_CLONE_TEXT")); text != "" {
		extraParams["demo_text"] = text
	}

	body := map[string]any{
		"speaker_id":        "custom_speaker_id",
		"custom_speaker_id": customSpeakerID,
		"audio": map[string]any{
			"data":   base64.StdEncoding.EncodeToString(audioBytes),
			"format": audioFormat,
		},
		"language": language,
	}
	if extra := strings.TrimSpace(os.Getenv("OPENSPEECH_VOICE_CLONE_EXTRA_PARAMS")); extra != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(extra), &parsed); err != nil {
			return VoiceCloneResult{}, fmt.Errorf("invalid OPENSPEECH_VOICE_CLONE_EXTRA_PARAMS: %w", err)
		}
		for key, value := range parsed {
			extraParams[key] = value
		}
	}
	if len(extraParams) > 0 {
		body["extra_params"] = extraParams
	}

	var resp map[string]any
	if err := c.postOpenSpeechJSON(ctx, "/api/v3/tts/voice_clone", cloneResourceID, body, &resp); err != nil {
		return VoiceCloneResult{}, fmt.Errorf("voice_clone custom_speaker_id=%s resource=%s model_type=%d: %w", customSpeakerID, cloneResourceID, modelType, err)
	}
	if err := openSpeechResponseError(resp); err != nil {
		return VoiceCloneResult{}, fmt.Errorf("voice_clone custom_speaker_id=%s resource=%s model_type=%d: %w", customSpeakerID, cloneResourceID, modelType, err)
	}

	result := VoiceCloneResult{
		TaskID:    getString(resp, "task_id"),
		VoiceID:   firstNonEmpty(getString(resp, "speaker_id"), customSpeakerID),
		VoiceType: firstNonEmpty(getString(resp, "voice_type"), getString(resp, "speaker_id"), customSpeakerID),
		Status:    "processing",
	}
	if result.VoiceID == "" {
		return VoiceCloneResult{}, errors.New("no speaker_id returned from voice clone")
	}

	status, demoAudio, err := c.waitVoiceReady(ctx, result.VoiceID)
	if err != nil {
		return VoiceCloneResult{}, fmt.Errorf("get_voice speaker_id=%s: %w", result.VoiceID, err)
	}
	if status == 2 || status == 4 {
		result.Status = "ready"
		result.PreviewAudioURL = demoAudio
	} else if status == 3 {
		result.Status = "failed"
	}
	return result, nil
}

func (c *ArkClient) GenerateVoicePreview(ctx context.Context, p VoicePreviewParams) (string, error) {
	if c.Mock {
		return mockAudioDataURL(), nil
	}
	text := strings.TrimSpace(p.Text)
	if text == "" {
		text = "这是音色试听。"
	}
	speaker := firstNonEmpty(p.VoiceType, p.VoiceID, os.Getenv("OPENSPEECH_DEFAULT_SPEAKER"))
	if speaker == "" {
		return "", errors.New("voice_type or voice_id required")
	}
	format := firstNonEmpty(p.Format, os.Getenv("OPENSPEECH_TTS_FORMAT"), "mp3")
	audio, err := c.SynthesizeSpeech(ctx, VoiceSynthesisParams{
		Text:       text,
		Speaker:    speaker,
		Format:     format,
		SampleRate: envInt("OPENSPEECH_TTS_SAMPLE_RATE", 24000),
		ResourceID: openSpeechResourceID("OPENSPEECH_TTS_RESOURCE_ID", resourceIDForSpeaker(speaker)),
	})
	if err != nil {
		return "", err
	}
	mime := "audio/mpeg"
	if format == "wav" {
		mime = "audio/wav"
	} else if format == "ogg_opus" || format == "ogg" {
		mime = "audio/ogg"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(audio), nil
}

type VoiceSynthesisParams struct {
	Text       string
	Speaker    string
	ResourceID string
	Format     string
	SampleRate int
}

func (c *ArkClient) SynthesizeSpeech(ctx context.Context, p VoiceSynthesisParams) ([]byte, error) {
	if c.Mock {
		return base64.StdEncoding.DecodeString(strings.TrimPrefix(mockAudioDataURL(), "data:audio/wav;base64,"))
	}
	text := strings.TrimSpace(p.Text)
	if text == "" {
		return nil, errors.New("text required")
	}
	speaker := strings.TrimSpace(p.Speaker)
	if speaker == "" {
		return nil, errors.New("speaker required")
	}
	format := firstNonEmpty(p.Format, "mp3")
	sampleRate := p.SampleRate
	if sampleRate == 0 {
		sampleRate = 24000
	}
	body := map[string]any{
		"user": map[string]any{"uid": "illustration2"},
		"req_params": map[string]any{
			"text":    text,
			"speaker": speaker,
			"audio_params": map[string]any{
				"format":      format,
				"sample_rate": sampleRate,
			},
		},
	}
	return c.postOpenSpeechTTS(ctx, "/api/v3/tts/unidirectional", firstNonEmpty(p.ResourceID, resourceIDForSpeaker(speaker)), body)
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

func (c *ArkClient) postOpenSpeechJSON(ctx context.Context, path, resourceID string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openSpeechBase()+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	setOpenSpeechHeaders(req, resourceID)
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("openspeech http %d logid=%s: %s", res.StatusCode, res.Header.Get("X-Tt-Logid"), string(bodyBytes))
	}
	return json.Unmarshal(bodyBytes, out)
}

func (c *ArkClient) postOpenSpeechTTS(ctx context.Context, path, resourceID string, body any) ([]byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openSpeechBase()+path, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	setOpenSpeechHeaders(req, resourceID)
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("openspeech http %d logid=%s: %s", res.StatusCode, res.Header.Get("X-Tt-Logid"), string(bodyBytes))
	}

	var audio bytes.Buffer
	dec := json.NewDecoder(res.Body)
	for {
		var msg map[string]any
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		code := getNumber(msg, "code")
		if code == 20000000 {
			break
		}
		if code != 0 {
			return nil, fmt.Errorf("tts failed code=%d message=%s logid=%s", code, getString(msg, "message"), res.Header.Get("X-Tt-Logid"))
		}
		data := getString(msg, "data")
		if data == "" {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			return nil, err
		}
		audio.Write(chunk)
	}
	if audio.Len() == 0 {
		return nil, errors.New("no audio returned")
	}
	return audio.Bytes(), nil
}

func (c *ArkClient) waitVoiceReady(ctx context.Context, speakerID string) (int, string, error) {
	deadline := time.Now().Add(time.Duration(envInt("OPENSPEECH_VOICE_CLONE_WAIT_SECONDS", 30)) * time.Second)
	for {
		status, demoAudio, err := c.getVoiceStatus(ctx, speakerID)
		if err != nil {
			return 0, "", err
		}
		if status == 2 || status == 3 || status == 4 || time.Now().After(deadline) {
			return status, demoAudio, nil
		}
		select {
		case <-ctx.Done():
			return 0, "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *ArkClient) getVoiceStatus(ctx context.Context, speakerID string) (int, string, error) {
	body := map[string]any{"speaker_id": speakerID}
	if !strings.HasPrefix(speakerID, "S_") {
		body["speaker_id"] = "custom_speaker_id"
		body["custom_speaker_id"] = speakerID
	}
	var resp map[string]any
	modelType := envInt("OPENSPEECH_VOICE_CLONE_MODEL_TYPE", 4)
	resourceID := voiceCloneResourceID(modelType)
	if err := c.postOpenSpeechJSON(ctx, "/api/v3/tts/get_voice", resourceID, body, &resp); err != nil {
		return 0, "", fmt.Errorf("resource=%s model_type=%d: %w", resourceID, modelType, err)
	}
	if err := openSpeechResponseError(resp); err != nil {
		return 0, "", fmt.Errorf("resource=%s model_type=%d: %w", resourceID, modelType, err)
	}
	return getNumber(resp, "status"), getString(resp, "demo_audio"), nil
}

func setOpenSpeechHeaders(req *http.Request, resourceID string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("X-Api-Request-Id", uuid.NewString())
	if apiKey := firstNonEmpty(os.Getenv("OPENSPEECH_API_KEY"), os.Getenv("VOLCENGINE_TTS_API_KEY"), os.Getenv("DOUBAO_API_KEY")); apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}
	if appID := firstNonEmpty(os.Getenv("OPENSPEECH_APP_ID"), os.Getenv("VOLCENGINE_TTS_APPID")); appID != "" {
		req.Header.Set("X-Api-App-Key", appID)
		req.Header.Set("X-Api-App-Id", appID)
	}
	if accessKey := firstNonEmpty(os.Getenv("OPENSPEECH_ACCESS_KEY"), os.Getenv("VOLCENGINE_TTS_ACCESS_KEY")); accessKey != "" {
		req.Header.Set("X-Api-Access-Key", accessKey)
	}
	if resourceID != "" {
		req.Header.Set("X-Api-Resource-Id", resourceID)
	}
}

func readAudioReference(ctx context.Context, client *http.Client, ref string) ([]byte, string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, "", errors.New("sample audio is required")
	}
	var data []byte
	var err error
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
		if reqErr != nil {
			return nil, "", reqErr
		}
		res, reqErr := client.Do(req)
		if reqErr != nil {
			return nil, "", reqErr
		}
		defer res.Body.Close()
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, "", fmt.Errorf("download sample audio http %d", res.StatusCode)
		}
		data, err = io.ReadAll(res.Body)
	} else {
		localPath := strings.TrimPrefix(ref, "/")
		data, err = os.ReadFile(localPath)
	}
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return nil, "", errors.New("sample audio is empty")
	}
	if len(data) > 10*1024*1024 {
		return nil, "", errors.New("sample audio exceeds 10MB OpenSpeech voice clone limit")
	}
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(ref)), ".")
	if format == "" {
		format = "wav"
	}
	if format == "oga" {
		format = "ogg"
	}
	return data, format, nil
}

func openSpeechBase() string {
	return strings.TrimRight(firstNonEmpty(os.Getenv("OPENSPEECH_BASE_URL"), "https://openspeech.bytedance.com"), "/")
}

func openSpeechResourceID(envName, fallback string) string {
	return firstNonEmpty(os.Getenv(envName), os.Getenv("OPENSPEECH_RESOURCE_ID"), fallback)
}

func resourceIDForSpeaker(speaker string) string {
	if strings.HasPrefix(speaker, "S_") {
		return voiceCloneResourceID(envInt("OPENSPEECH_VOICE_CLONE_MODEL_TYPE", 4))
	}
	if strings.Contains(speaker, "_uranus_bigtts") || strings.HasPrefix(speaker, "saturn_") {
		return "seed-tts-2.0"
	}
	return "seed-tts-1.0"
}

func voiceCloneResourceID(modelType int) string {
	return openSpeechResourceID("OPENSPEECH_VOICE_CLONE_RESOURCE_ID", "volc.megatts.timbre")
}

func resourceIDForCloneModelType(modelType int) string {
	if modelType >= 1 && modelType <= 3 {
		return "seed-icl-1.0"
	}
	return "seed-icl-2.0"
}

func openSpeechResponseError(resp map[string]any) error {
	if baseResp, ok := resp["BaseResp"].(map[string]any); ok {
		if code := getNumber(baseResp, "StatusCode"); code != 0 {
			return fmt.Errorf("openspeech failed code=%d message=%s", code, getString(baseResp, "StatusMessage"))
		}
	}
	if code := getNumber(resp, "code"); code != 0 {
		return fmt.Errorf("openspeech failed code=%d message=%s", code, getString(resp, "message"))
	}
	return nil
}

func mockAudioDataURL() string {
	return "data:audio/wav;base64,UklGRiQAAABXQVZFZm10IBAAAAABAAEAESsAACJWAAACABAAZGF0YQAAAAA="
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func getString(m map[string]any, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getNumber(m map[string]any, k string) int {
	if v, ok := m[k]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		case json.Number:
			i, _ := n.Int64()
			return int(i)
		}
	}
	return 0
}

func (c *ArkClient) ChatJSON(ctx context.Context, model string, prompt string) (string, error) {
	if c.Mock {
		return "A warm children's book animation shot with consistent character appearance, gentle camera movement, expressive action, soft natural light, no subtitles, no text, no watermark.", nil
	}
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
