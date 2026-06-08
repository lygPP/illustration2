package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"illustration2/internal/volc"

	"github.com/gin-gonic/gin"
)

type personaRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ImageURL    string `json:"image_url"`
}

type voiceRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type voicePreviewRequest struct {
	Text string `json:"text"`
}

func (h *Handler) ListPersonas(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"personas": h.store.ListPersonas(user.ID)})
}

func (h *Handler) CreatePersona(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req personaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	persona, err := h.store.CreatePersona(user.ID, PersonaInput{
		Name:        req.Name,
		Description: req.Description,
		ImageURL:    req.ImageURL,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"persona": persona})
}

func (h *Handler) UpdatePersona(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req personaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	persona, err := h.store.UpdatePersona(user.ID, c.Param("persona_id"), PersonaInput{
		Name:        req.Name,
		Description: req.Description,
		ImageURL:    req.ImageURL,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"persona": persona})
}

func (h *Handler) DeletePersona(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err := h.store.DeletePersona(user.ID, c.Param("persona_id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) UploadPersonaImage(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	url, err := saveUpload(c, "image", filepath.Join("uploads", "personas", user.ID), user.ID, 6*1024*1024, map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	persona, err := h.store.UpdatePersonaImage(user.ID, c.Param("persona_id"), url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"persona": persona})
}

func (h *Handler) ListVoices(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"voices": h.store.ListVoices(user.ID)})
}

func (h *Handler) GetVoice(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	voice, err := h.store.GetVoice(user.ID, c.Param("voice_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"voice": voice})
}

func (h *Handler) CreateVoice(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req voiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	voice, err := h.store.CreateVoice(user.ID, VoiceInput{Name: req.Name, Description: req.Description})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"voice": voice})
}

func (h *Handler) CreateClonedVoice(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	name := strings.TrimSpace(c.PostForm("name"))
	description := strings.TrimSpace(c.PostForm("description"))
	previewText := strings.TrimSpace(c.PostForm("preview_text"))
	speakerID := strings.TrimSpace(c.PostForm("speaker_id"))
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name required"})
		return
	}

	sampleURL, err := saveUpload(c, "sample", filepath.Join("uploads", "voices", user.ID), user.ID, 20*1024*1024, map[string]bool{
		".wav": true, ".mp3": true, ".m4a": true, ".aac": true, ".ogg": true, ".webm": true,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	client := h.arkClient
	if client == nil {
		client = volc.NewArkClientDefault()
	}
	result, err := client.CloneVoice(context.Background(), volc.VoiceCloneParams{
		Name:           name,
		Description:    description,
		SampleAudioURL: sampleURL,
		SpeakerID:      speakerID,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if previewText != "" && result.VoiceType != "" {
		if previewURL, previewErr := client.GenerateVoicePreview(context.Background(), volc.VoicePreviewParams{
			Text:      previewText,
			VoiceType: result.VoiceType,
			VoiceID:   result.VoiceID,
		}); previewErr == nil {
			result.PreviewAudioURL = previewURL
		}
	}

	voice, err := h.store.CreateVoice(user.ID, VoiceInput{Name: name, Description: description})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	voice, err = h.store.UpdateVoiceSample(user.ID, voice.ID, sampleURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	voice, err = h.store.ApplyVoiceCloneResult(user.ID, voice.ID, VoiceCloneResult{
		ArkTaskID:        result.TaskID,
		GeneratedVoiceID: result.VoiceID,
		VoiceType:        result.VoiceType,
		PreviewAudioURL:  result.PreviewAudioURL,
		Status:           result.Status,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"voice": voice})
}

func (h *Handler) UpdateVoice(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req voiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	voice, err := h.store.UpdateVoice(user.ID, c.Param("voice_id"), VoiceInput{Name: req.Name, Description: req.Description})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"voice": voice})
}

func (h *Handler) DeleteVoice(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err := h.store.DeleteVoice(user.ID, c.Param("voice_id")); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *Handler) UploadVoiceSample(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	url, err := saveUpload(c, "sample", filepath.Join("uploads", "voices", user.ID), user.ID, 20*1024*1024, map[string]bool{
		".wav": true, ".mp3": true, ".m4a": true, ".aac": true, ".ogg": true, ".webm": true,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	voice, err := h.store.UpdateVoiceSample(user.ID, c.Param("voice_id"), url)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"voice": voice})
}

func (h *Handler) CloneVoice(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	voice, err := h.store.GetVoice(user.ID, c.Param("voice_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if voice.SampleAudioURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sample audio is required"})
		return
	}
	client := h.arkClient
	if client == nil {
		client = volc.NewArkClientDefault()
	}
	result, err := client.CloneVoice(context.Background(), volc.VoiceCloneParams{
		Name:           voice.Name,
		Description:    voice.Description,
		SampleAudioURL: voice.SampleAudioURL,
	})
	if err != nil {
		nextVoice, _ := h.store.ApplyVoiceCloneResult(user.ID, voice.ID, VoiceCloneResult{Status: "failed", ErrorMessage: err.Error()})
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "voice": nextVoice})
		return
	}
	nextVoice, err := h.store.ApplyVoiceCloneResult(user.ID, voice.ID, VoiceCloneResult{
		ArkTaskID:        result.TaskID,
		GeneratedVoiceID: result.VoiceID,
		VoiceType:        result.VoiceType,
		PreviewAudioURL:  result.PreviewAudioURL,
		Status:           result.Status,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"voice": nextVoice})
}

func (h *Handler) PreviewVoice(c *gin.Context) {
	user, ok := CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req voicePreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	voice, err := h.store.GetVoice(user.ID, c.Param("voice_id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	client := h.arkClient
	if client == nil {
		client = volc.NewArkClientDefault()
	}
	previewURL, err := client.GenerateVoicePreview(context.Background(), volc.VoicePreviewParams{
		Text:      req.Text,
		VoiceType: voice.VoiceType,
		VoiceID:   voice.GeneratedVoiceID,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	nextVoice, err := h.store.ApplyVoiceCloneResult(user.ID, voice.ID, VoiceCloneResult{
		PreviewAudioURL: previewURL,
		Status:          "ready",
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"voice": nextVoice})
}

func saveUpload(c *gin.Context, field, dir, prefix string, maxSize int64, allowedExt map[string]bool) (string, error) {
	file, err := c.FormFile(field)
	if err != nil {
		return "", fmt.Errorf("%s file is required", field)
	}
	if file.Size > maxSize {
		return "", fmt.Errorf("%s file is too large", field)
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if !allowedExt[ext] {
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("%s_%d%s", prefix, time.Now().UnixNano(), ext)
	dst := filepath.Join(dir, filename)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(dst), nil
}
