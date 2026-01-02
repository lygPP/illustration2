package main

import (
	"bufio"
	"context"
	"fmt"
	"illustration2/internal/config"
	"illustration2/internal/ill_agent"
	"log"

	// "net/http"
	"os"
	// "os/signal"
	// "syscall"

	"github.com/cloudwego/eino-examples/adk/common/prints"
	"github.com/cloudwego/eino-examples/adk/common/store"
	"github.com/cloudwego/eino/adk"
	// "github.com/gin-gonic/gin"
	// "github.com/sirupsen/logrus"
)

func main() {
	config.InitConfig()

	ctx := context.Background()
	debugAgent(ctx)
	// feedback_loop_example.Main_exec()
	// // 初始化日志
	// logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	// logrus.SetLevel(logrus.InfoLevel)

	// // 初始化Gin路由
	// router := gin.Default()

	// // 启动服务器
	// srv := &http.Server{
	// 	Addr:    ":8080",
	// 	Handler: router,
	// }

	// // 在goroutine中启动服务器
	// go func() {
	// 	log.Printf("服务器启动在 :8080")
	// 	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
	// 		log.Fatalf("启动服务器失败: %v", err)
	// 	}
	// }()

	// // 等待中断信号
	// quit := make(chan os.Signal, 1)
	// signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	// <-quit
	// log.Println("关闭服务器...")

	// // 优雅关闭服务器
	// if err := srv.Close(); err != nil {
	// 	log.Fatalf("服务器关闭失败: %v", err)
	// }

	// log.Println("服务器已关闭")
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
