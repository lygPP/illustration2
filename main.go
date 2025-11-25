package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"illustration2/internal/service"
	"illustration2/internal/tools"
	"illustration2/internal/volc"

	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()

	ark := volc.NewArkClientDefault()
	svc := service.NewStoryVideoService(ark)
	seedTool := tools.NewSeedreamTool(ark)
	seedanceTool := tools.NewSeedanceTool(ark)

	router.POST("/story-video", func(c *gin.Context) {
		var req struct {
			Theme    string `json:"theme"`
			Chapters int    `json:"chapters"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.Chapters <= 0 {
			req.Chapters = 3
		}

		apiKey := os.Getenv("ARK_API_KEY")
		if apiKey == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing ARK_API_KEY env"})
			return
		}

		ctx := c.Request.Context()
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()

		result, err := svc.Run(ctx, req.Theme, req.Chapters)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"video_url": result.VideoURL})
	})

	router.POST("/eino/seedream", func(c *gin.Context) {
		var m map[string]any
		if err := c.ShouldBindJSON(&m); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		b, _ := json.Marshal(m)
		out, err := seedTool.InvokableRun(c.Request.Context(), string(b))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var resp any
		if json.Unmarshal([]byte(out), &resp) == nil {
			c.JSON(http.StatusOK, resp)
			return
		}
		c.JSON(http.StatusOK, resp)
	})

	// 注册视频生成工具路由
	router.POST("/eino/seedance", func(c *gin.Context) {
		var m map[string]any
		if err := c.ShouldBindJSON(&m); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		b, _ := json.Marshal(m)
		out, err := seedanceTool.InvokableRun(c.Request.Context(), string(b))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		var resp any
		if json.Unmarshal([]byte(out), &resp) == nil {
			c.JSON(http.StatusOK, resp)
			return
		}
		c.JSON(http.StatusOK, resp)
	})

	if err := router.Run(":8080"); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
