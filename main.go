package main

import (
	"bufio"
	"context"
	"fmt"
	"illustration2/internal/auth"
	"illustration2/internal/config"
	"illustration2/internal/handler"
	"illustration2/internal/ill_agent"
	"illustration2/internal/service"
	"illustration2/internal/volc"
	"log"
	"net/http"
	"os/signal"
	"syscall"

	"os"

	"github.com/cloudwego/eino-examples/adk/common/prints"
	"github.com/cloudwego/eino-examples/adk/common/store"
	"github.com/cloudwego/eino/adk"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func main() {
	config.InitConfig()

	// ctx := context.Background()
	// ill_agent.TestImageAgent(ctx)
	// debugAgent(ctx)
	// feedback_loop_example.Main_exec()
	// 初始化日志
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)

	// 初始化Gin路由
	router := gin.Default()
	router.Static("/uploads", "./uploads")
	router.Static("/resource", "./resource")

	// 初始化服务
	arkClient := volc.NewArkClientDefault()
	appStore, err := auth.NewStore("")
	if err != nil {
		log.Fatalf("初始化用户数据失败: %v", err)
	}
	if err := appStore.EnsureSuperAdmin(); err != nil {
		log.Fatalf("初始化超级管理员失败: %v", err)
	}
	genService := service.NewGenerationService(arkClient)
	authHandler := auth.NewHandler(appStore, arkClient)
	genHandler := handler.NewGenerationHandler(genService, appStore)
	agentStreamHandler := handler.NewAgentStreamHandler(genService, appStore)

	api := router.Group("/api")
	api.POST("/auth/register", authHandler.Register)
	api.POST("/auth/login", authHandler.Login)

	protected := api.Group("/")
	protected.Use(auth.Middleware(appStore))
	protected.GET("/me", authHandler.Me)
	protected.PUT("/me", authHandler.UpdateMe)
	protected.POST("/me/avatar", authHandler.UploadAvatar)
	protected.GET("/me/history", authHandler.History)
	protected.GET("/me/usage", authHandler.Usage)
	protected.GET("/personas", authHandler.ListPersonas)
	protected.POST("/personas", authHandler.CreatePersona)
	protected.PUT("/personas/:persona_id", authHandler.UpdatePersona)
	protected.DELETE("/personas/:persona_id", authHandler.DeletePersona)
	protected.POST("/personas/:persona_id/image", authHandler.UploadPersonaImage)
	protected.GET("/voices", authHandler.ListVoices)
	protected.POST("/voices", authHandler.CreateVoice)
	protected.POST("/voices/clone-create", authHandler.CreateClonedVoice)
	protected.GET("/voices/:voice_id", authHandler.GetVoice)
	protected.PUT("/voices/:voice_id", authHandler.UpdateVoice)
	protected.DELETE("/voices/:voice_id", authHandler.DeleteVoice)
	protected.POST("/voices/:voice_id/sample", authHandler.UploadVoiceSample)
	protected.POST("/voices/:voice_id/clone", authHandler.CloneVoice)
	protected.POST("/voices/:voice_id/preview", authHandler.PreviewVoice)
	protected.POST("/generate", genHandler.HandleGeneration)
	protected.GET("/video/:task_id", genHandler.HandleGetVideo)
	protected.POST("/agent/stream", agentStreamHandler.HandleAgentStream)
	protected.POST("/agent/resume", agentStreamHandler.HandleAgentResume)

	admin := protected.Group("/admin")
	admin.Use(auth.RequireSuperAdmin())
	admin.GET("/users", authHandler.AdminListUsers)
	admin.POST("/users", authHandler.AdminCreateUser)
	admin.GET("/users/:user_id", authHandler.AdminGetUser)
	admin.PUT("/users/:user_id", authHandler.AdminUpdateUser)
	admin.POST("/users/:user_id/reset-password", authHandler.AdminResetPassword)
	admin.DELETE("/users/:user_id", authHandler.AdminDeleteUser)

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

func debugAgent(ctx context.Context) {
	a := ill_agent.NewMKAgent(ctx)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: true, // you can disable streaming here
		Agent:           a,
		CheckPointStore: store.NewInMemoryStore(),
	})
	iter := runner.Query(ctx, "恐龙为什么灭绝了？", adk.WithCheckPointID("1"))

	for {
		var lastEvent *adk.AgentEvent
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}
			if event.Err != nil {
				log.Fatal(event.Err)
			}

			prints.Event(event)

			lastEvent = event
		}

		if lastEvent == nil {
			log.Fatal("last event is nil")
		}

		if lastEvent.Action != nil && lastEvent.Action.Exit {
			return
		}

		if lastEvent.Action == nil || lastEvent.Action.Interrupted == nil {
			log.Fatal("last event is not an interrupt event")
		}

		// reInfo := lastEvent.Action.Interrupted.InterruptContexts[0].Info.(string)
		interruptID := lastEvent.Action.Interrupted.InterruptContexts[0].ID

		nInput := ""
		for {
			scanner := bufio.NewScanner(os.Stdin)
			fmt.Print("your input here: ")
			scanner.Scan()
			fmt.Println()
			nInput = scanner.Text()
			break
		}

		var err error
		iter, err = runner.ResumeWithParams(ctx, "1", &adk.ResumeParams{
			Targets: map[string]any{
				interruptID: nInput,
			},
		})
		if err != nil {
			log.Fatal(err)
		}
	}
}
