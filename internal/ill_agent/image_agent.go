package ill_agent

import (
	"bufio"
	"context"
	"fmt"
	"illustration2/internal/model"
	"log"
	"os"

	"github.com/cloudwego/eino-examples/adk/common/prints"
	"github.com/cloudwego/eino-examples/adk/common/store"
	"github.com/cloudwego/eino/adk"
)

func NewImageAgent(ctx context.Context) adk.Agent {
	imageLoopAgent, err := adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
		Name:        "图片生成&审核agent",
		Description: "一个可以生成图片&可持续根据反馈优化重新生成图片的agent",
		SubAgents: []adk.Agent{
			NewImageGenerateAgent(ctx),
			NewImageReviewAgent(ctx),
		},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create imageLoopAgent: %w", err))
	}
	la, err := adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
		Name:        "图片小助手",
		Description: "一个图片生成助手",
		SubAgents: []adk.Agent{
			NewImagePromptAgent(ctx),
			imageLoopAgent,
		},
	})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to create sequentialagent: %w", err))
	}

	return la
}

func TestImageAgent(ctx context.Context) {
	sessionState := GetSessionState(ctx)
	sessionState.Story = &model.Story{
		Theme: "恐龙为什么灭绝了？",
		Chapters: []model.StoryChapter{
			{
				Title:   "第1章：恐龙时代",
				Content: "很久很久以前，地球上有许多巨大的恐龙。它们有的长着长长的脖子，有的长着锋利的牙齿，有的会飞，有的会游泳。那时候，恐龙是地球上的霸主，它们自由自在地生活着。",
			},
			{
				Title:   "第2章：奇怪的变化",
				Content: "可是有一天，天空突然变得灰蒙蒙的。一颗巨大的小行星撞上了地球，扬起了厚厚的灰尘。太阳被遮住了，植物开始枯萎，天气也变得非常寒冷。恐龙们找不到足够的食物，也很难适应这突然的变化。",
			},
			{
				Title:   "第3章：恐龙的消失",
				Content: "渐渐地，巨大的恐龙越来越虚弱。它们无法在寒冷和黑暗中生存下去，最终一只接一只地消失了。但别担心，它们的化石留了下来，让我们今天还能在博物馆里看到它们的故事！",
			},
		},
	}
	SaveSessionState(ctx, sessionState)

	a := NewImageAgent(ctx)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: true, // you can disable streaming here
		Agent:           a,
		CheckPointStore: store.NewInMemoryStore(),
	})
	iter := runner.Query(ctx, "开始生成图片", adk.WithCheckPointID("1"))

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
