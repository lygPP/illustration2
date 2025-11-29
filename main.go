package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"illustration2/internal/agent"
	"illustration2/internal/tools"
	"illustration2/internal/volc"
)

func main() {
	// 初始化日志
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)

	// 初始化ArkClient
	arkClient := volc.NewArkClientDefault()

	// 初始化工具
	imageTool := tools.NewSeedreamTool(arkClient)
	videoTool := tools.NewSeedanceTool(arkClient)
	storyTool := tools.NewStoryTool(arkClient)

	// 初始化ChildIllustrationAgent
	illustrationAgent := agent.NewChildIllustrationAgent(arkClient, storyTool, imageTool, videoTool)

	// 初始化Gin路由
	router := gin.Default()

	// 添加路由
	router.POST("/agent/child-illustration", handleAgentRequest(illustrationAgent))
	router.GET("/agent/child-illustration/info", handleAgentInfo(illustrationAgent))
	router.POST("/tools/story-generate", handleStoryGenerate(storyTool))

	// 启动服务器
	srv := &http.Server{
		Addr:    ":8080",
		Handler: router,
	}

	// 在goroutine中启动服务器
	go func() {
		log.Printf("服务器启动在 :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("启动服务器失败: %v", err)
		}
	}()

	// 等待中断信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("关闭服务器...")

	// 优雅关闭服务器
	if err := srv.Close(); err != nil {
		log.Fatalf("服务器关闭失败: %v", err)
	}

	log.Println("服务器已关闭")
}

// generateSessionID 生成会话ID
func generateSessionID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("session_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// handleAgentRequest 处理agent请求
func handleAgentRequest(illustrationAgent *agent.ChildIllustrationAgent) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			SessionID string `json:"session_id"`
			Input     string `json:"input"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求格式"})
			return
		}

		// 如果没有提供sessionID，生成一个新的
		if req.SessionID == "" {
			req.SessionID = generateSessionID()
		}

		// 执行agent
		response, state, err := illustrationAgent.Execute(c.Request.Context(), req.SessionID, req.Input)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("执行agent失败: %v", err)})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"session_id": req.SessionID,
			"response":   response,
			"state":      state,
		})
	}
}

// handleAgentInfo 处理agent信息请求
func handleAgentInfo(illustrationAgent *agent.ChildIllustrationAgent) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, illustrationAgent.Info())
	}
}

// handleStoryGenerate 处理故事生成请求
func handleStoryGenerate(storyTool *tools.StoryTool) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 直接读取请求体作为JSON参数
		var body []byte
		if err := c.ShouldBindBodyWithJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求格式"})
			return
		}

		// 调用工具生成故事
		result, err := storyTool.InvokableRun(c.Request.Context(), string(body))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("生成故事失败: %v", err)})
			return
		}

		// 返回生成的故事
		c.Data(http.StatusOK, "application/json", []byte(result))
	}
}
