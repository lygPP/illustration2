package handler

import (
	"illustration2/internal/auth"
	"illustration2/internal/service"
	"illustration2/internal/usage"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type GenerationHandler struct {
	svc   *service.GenerationService
	store *auth.Store
}

func NewGenerationHandler(svc *service.GenerationService, store *auth.Store) *GenerationHandler {
	return &GenerationHandler{svc: svc, store: store}
}

func (h *GenerationHandler) HandleGeneration(c *gin.Context) {
	var req service.GenerationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx := usage.WithContext(c.Request.Context(), user.ID, h.store)
	resp, err := h.svc.Generate(ctx, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	modelName := strings.TrimSpace(req.ModelName)
	if modelName == "" {
		modelName = req.GenerateResourceType
	}
	_ = h.store.AddHistory(user.ID, auth.GenerationHistory{
		Kind:         "generate",
		ResourceType: req.GenerateResourceType,
		ModelName:    modelName,
		Prompt:       req.Prompt,
		Status:       generationStatus(resp),
		TaskID:       resp.TaskID,
		PreviewURL:   previewURL(resp),
		Summary:      generationSummary(resp),
	})

	c.JSON(http.StatusOK, resp)
}

func (h *GenerationHandler) HandleGetVideo(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id is required"})
		return
	}
	if !h.store.UserOwnsTask(user.ID, taskID) {
		c.JSON(http.StatusForbidden, gin.H{"error": "task does not belong to current user"})
		return
	}

	resp, err := h.svc.GetVideoResult(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func generationStatus(resp *service.GenerationResponse) string {
	if resp == nil {
		return "unknown"
	}
	if resp.TaskID != "" {
		return "processing"
	}
	return "succeeded"
}

func generationSummary(resp *service.GenerationResponse) string {
	if resp == nil {
		return ""
	}
	if resp.Type == "image" {
		return "generated " + strconv.Itoa(len(resp.Images)) + " image(s)"
	}
	if resp.Type == "video" && resp.TaskID != "" {
		return "video task created"
	}
	return resp.Message
}

func previewURL(resp *service.GenerationResponse) string {
	if resp == nil || len(resp.Images) == 0 {
		return ""
	}
	if len(resp.Images[0]) > 2048 {
		return ""
	}
	return resp.Images[0]
}
