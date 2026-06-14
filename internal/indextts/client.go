package indextts

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "http://127.0.0.1:7860"

type Client struct {
	BaseURL    string
	Path       string
	TextField  string
	AudioField string
	HTTPClient *http.Client
	Mock       bool
}

type SynthesisParams struct {
	Text              string
	ReferenceAudioURL string
}

func NewClientFromEnv() *Client {
	timeoutSeconds := envInt("INDEX_TTS_TIMEOUT_SECONDS", 1200)
	path := firstNonEmpty(os.Getenv("INDEX_TTS_SYNTHESIS_PATH"), "/tts")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return &Client{
		BaseURL:    strings.TrimRight(firstNonEmpty(os.Getenv("INDEX_TTS_BASE_URL"), defaultBaseURL), "/"),
		Path:       path,
		TextField:  firstNonEmpty(os.Getenv("INDEX_TTS_TEXT_FIELD"), "text"),
		AudioField: firstNonEmpty(os.Getenv("INDEX_TTS_AUDIO_FIELD"), "reference_audio"),
		HTTPClient: &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		Mock:       isMock(),
	}
}

func (c *Client) Synthesize(ctx context.Context, p SynthesisParams) ([]byte, error) {
	if c.Mock {
		return mockAudioBytes()
	}
	text := strings.TrimSpace(p.Text)
	if text == "" {
		return nil, errors.New("text required")
	}
	refPath, cleanup, err := c.referenceAudioPath(ctx, p.ReferenceAudioURL)
	if err != nil {
		return nil, err
	}
	if cleanup != nil {
		defer cleanup()
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField(c.TextField, text); err != nil {
		return nil, err
	}
	file, err := os.Open(refPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	part, err := writer.CreateFormFile(c.AudioField, filepath.Base(refPath))
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+c.Path, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		if res.StatusCode == http.StatusNotFound && c.Path == "/tts" {
			return c.synthesizeWithGradio(ctx, text, refPath)
		}
		return nil, fmt.Errorf("index tts http %d: %s", res.StatusCode, truncate(string(resBody), 500))
	}

	contentType := strings.ToLower(res.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "audio/") || !strings.Contains(contentType, "json") {
		if len(resBody) == 0 {
			return nil, errors.New("index tts returned empty audio")
		}
		return resBody, nil
	}
	return c.audioFromJSON(ctx, resBody)
}

func (c *Client) synthesizeWithGradio(ctx context.Context, text, refPath string) ([]byte, error) {
	uploadedPath, err := c.uploadGradioFile(ctx, refPath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(refPath)
	if err != nil {
		return nil, err
	}
	sessionHash := fmt.Sprintf("codex_%d", time.Now().UnixNano())
	fileData := map[string]any{
		"path":      uploadedPath,
		"url":       nil,
		"orig_name": filepath.Base(refPath),
		"size":      info.Size(),
		"mime_type": mimeTypeForAudio(refPath),
		"meta": map[string]any{
			"_type": "gradio.FileData",
		},
	}
	payload := map[string]any{
		"data": []any{
			"Same as the voice reference",
			fileData,
			text,
			nil,
			0.65,
			0, 0, 0, 0, 0, 0, 0, 0,
			"",
			false,
			120,
			true,
			0.8,
			30,
			0.8,
			0,
			3,
			10,
			1500,
		},
		"event_data":   nil,
		"fn_index":     9,
		"trigger_id":   75,
		"session_hash": sessionHash,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/gradio_api/queue/join", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	resBody, readErr := io.ReadAll(res.Body)
	res.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("index tts gradio queue join http %d: %s", res.StatusCode, truncate(string(resBody), 500))
	}

	audioRef, err := c.waitGradioResult(ctx, sessionHash)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(audioRef, "http://") || strings.HasPrefix(audioRef, "https://") || strings.HasPrefix(audioRef, "/") {
		return c.downloadAudio(ctx, audioRef)
	}
	audio, err := os.ReadFile(audioRef)
	if err != nil {
		return nil, err
	}
	if len(audio) == 0 {
		return nil, errors.New("index tts gradio returned empty audio")
	}
	return audio, nil
}

func (c *Client) uploadGradioFile(ctx context.Context, refPath string) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := os.Open(refPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	part, err := writer.CreateFormFile("files", filepath.Base(refPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/gradio_api/upload", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("index tts gradio upload http %d: %s", res.StatusCode, truncate(string(resBody), 500))
	}
	var uploaded []string
	if err := json.Unmarshal(resBody, &uploaded); err != nil {
		return "", err
	}
	if len(uploaded) == 0 || strings.TrimSpace(uploaded[0]) == "" {
		return "", errors.New("index tts gradio upload returned no file path")
	}
	return strings.TrimSpace(uploaded[0]), nil
}

func (c *Client) waitGradioResult(ctx context.Context, sessionHash string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/gradio_api/queue/data?session_hash="+sessionHash, nil)
	if err != nil {
		return "", err
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		resBody, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("index tts gradio queue data http %d: %s", res.StatusCode, truncate(string(resBody), 500))
	}

	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
			continue
		}
		msg, _ := event["msg"].(string)
		if msg != "process_completed" {
			continue
		}
		if success, ok := event["success"].(bool); ok && !success {
			return "", fmt.Errorf("index tts gradio process failed: %v", event["output"])
		}
		return audioRefFromGradioOutput(event["output"])
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("index tts gradio stream ended without audio")
}

func audioRefFromGradioOutput(output any) (string, error) {
	payload, ok := output.(map[string]any)
	if !ok {
		return "", errors.New("index tts gradio output is invalid")
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) == 0 {
		return "", errors.New("index tts gradio output contains no data")
	}
	item, ok := data[0].(map[string]any)
	if !ok {
		return "", errors.New("index tts gradio output item is invalid")
	}
	if value, ok := item["value"].(map[string]any); ok {
		if ref := stringFromAny(value["url"]); ref != "" {
			return ref, nil
		}
		if ref := stringFromAny(value["path"]); ref != "" {
			return ref, nil
		}
	}
	if ref := stringFromAny(item["url"]); ref != "" {
		return ref, nil
	}
	if ref := stringFromAny(item["path"]); ref != "" {
		return ref, nil
	}
	return "", errors.New("index tts gradio output does not contain audio")
}

func (c *Client) audioFromJSON(ctx context.Context, resBody []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(resBody, &payload); err != nil {
		return nil, err
	}

	for _, key := range []string{"audio_base64", "audio"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			value = strings.TrimSpace(value)
			if strings.HasPrefix(value, "data:") {
				if idx := strings.Index(value, ","); idx >= 0 {
					value = value[idx+1:]
				}
			}
			if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "/") {
				return c.downloadAudio(ctx, value)
			}
			audio, err := base64.StdEncoding.DecodeString(value)
			if err == nil && len(audio) > 0 {
				return audio, nil
			}
		}
	}
	for _, key := range []string{"audio_url", "url"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return c.downloadAudio(ctx, strings.TrimSpace(value))
		}
	}
	return nil, errors.New("index tts json response does not contain audio")
}

func (c *Client) downloadAudio(ctx context.Context, ref string) ([]byte, error) {
	if strings.HasPrefix(ref, "/") {
		ref = c.BaseURL + ref
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("download index tts audio http %d", res.StatusCode)
	}
	audio, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if len(audio) == 0 {
		return nil, errors.New("downloaded index tts audio is empty")
	}
	return audio, nil
}

func (c *Client) referenceAudioPath(ctx context.Context, ref string) (string, func(), error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil, errors.New("reference audio required")
	}
	if strings.HasPrefix(ref, "/") {
		path := strings.TrimPrefix(ref, "/")
		if _, err := os.Stat(path); err != nil {
			return "", nil, err
		}
		return path, nil, nil
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		audio, err := c.downloadAudio(ctx, ref)
		if err != nil {
			return "", nil, err
		}
		tmpDir, err := os.MkdirTemp("", "index_tts_ref_*")
		if err != nil {
			return "", nil, err
		}
		path := filepath.Join(tmpDir, "reference_audio")
		if err := os.WriteFile(path, audio, 0644); err != nil {
			os.RemoveAll(tmpDir)
			return "", nil, err
		}
		return path, func() { os.RemoveAll(tmpDir) }, nil
	}
	if _, err := os.Stat(ref); err != nil {
		return "", nil, err
	}
	return ref, nil, nil
}

func isMock() bool {
	value := strings.ToLower(os.Getenv("ARK_MOCK"))
	return value == "1" || value == "true"
}

func mockAudioBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString("UklGRiQAAABXQVZFZm10IBAAAAABAAEAESsAACJWAAACABAAZGF0YQAAAAA=")
}

func envInt(key string, fallback int) int {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func mimeTypeForAudio(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	case ".ogg":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
