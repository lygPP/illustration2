package handler

import (
	"illustration2/internal/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

type GenerationHandler struct {
	svc *service.GenerationService
}

func NewGenerationHandler(svc *service.GenerationService) *GenerationHandler {
	return &GenerationHandler{svc: svc}
}

func (h *GenerationHandler) HandleGeneration(c *gin.Context) {
	var req service.GenerationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.svc.Generate(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

func (h *GenerationHandler) HandleGetVideo(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id is required"})
		return
	}

	resp, err := h.svc.GetVideoResult(c.Request.Context(), taskID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}
