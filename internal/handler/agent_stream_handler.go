package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"illustration2/internal/ill_agent"
	"illustration2/internal/service"
	"log"
	"net/http"
	"sync"

	"github.com/cloudwego/eino-examples/adk/common/store"
	"github.com/cloudwego/eino/adk"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type AgentStreamHandler struct {
	genService *service.GenerationService
	sessions   map[string]*agentSession
	sessionsMu sync.RWMutex
}

type agentSession struct {
	runner *adk.Runner
}

func NewAgentStreamHandler(genService *service.GenerationService) *AgentStreamHandler {
	return &AgentStreamHandler{
		genService: genService,
		sessions:   make(map[string]*agentSession),
	}
}

type AgentStreamRequest struct {
	Theme string `json:"theme"`
}

type AgentStreamEvent struct {
	Type      string      `json:"type"`    // event, error, complete
	Data      interface{} `json:"data"`    // event data
	Message   string      `json:"message"` // optional message
	SessionID string      `json:"session_id"`
}

// eventData is used to marshal adk.AgentEvent to JSON
type eventData struct {
	AgentName   string      `json:"agent_name,omitempty"`
	IsStreaming bool        `json:"is_streaming,omitempty"`
	Message     string      `json:"message,omitempty"`
	Output      interface{} `json:"output,omitempty"`
	Err         string      `json:"err,omitempty"`
	Action      string      `json:"action,omitempty"`
}

func (h *AgentStreamHandler) HandleAgentStream(c *gin.Context) {
	var req AgentStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	theme := req.Theme
	if theme == "" {
		theme = "恐龙为什么灭绝了？"
	}

	// Generate session ID
	sessionID := uuid.New().String()

	// Set headers for SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a context that will be canceled if client disconnects
	ctx, _ := context.WithCancel(c.Request.Context())

	// Channel to receive events from the agent
	eventChan := make(chan *adk.AgentEvent, 100)
	doneChan := make(chan struct{})

	// Create agent and runner
	a := ill_agent.NewMKAgent(ctx)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: true,
		Agent:           a,
		CheckPointStore: store.NewInMemoryStore(),
	})

	// Start query
	iter := runner.Query(ctx, theme, adk.WithCheckPointID(sessionID))

	// Store session
	session := &agentSession{
		runner: runner,
	}
	h.sessionsMu.Lock()
	h.sessions[sessionID] = session
	h.sessionsMu.Unlock()

	// Start the agent in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		// Process events
		for {
			event, ok := iter.Next()
			if !ok {
				break
			}

			select {
			case eventChan <- event:
			case <-ctx.Done():
				return
			}
		}

		close(doneChan)
	}()

	defer close(eventChan)

	// Flusher to send data immediately
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	// Send initial connected event with session ID
	sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
		Type:      "connected",
		Message:   "Agent started, processing theme: " + theme,
		SessionID: sessionID,
	})

	// Process events and send to client
	var finalData eventData
	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
					Type:      "complete",
					Message:   "Agent execution completed",
					SessionID: sessionID,
				})
				return
			}

			// Convert event to our data structure
			data := eventData{}
			data.AgentName = event.AgentName
			if event.Output != nil && event.Output.MessageOutput != nil {
				data.IsStreaming = event.Output.MessageOutput.IsStreaming
				if event.Output.MessageOutput.Message != nil {
					data.Message = event.Output.MessageOutput.Message.Content
				}
				data.Output = event.Output
			}
			if event.Err != nil {
				data.Err = event.Err.Error()
			}
			if event.Action != nil {
				if event.Action.Exit {
					data.Action = "exit"
				} else if event.Action.Interrupted != nil {
					data.Action = "interrupted"
				} else {
					data.Action = "normal_exec"
				}
			}
			if event.Action != nil && event.Action.Interrupted != nil && len(event.Action.Interrupted.InterruptContexts) > 0 {
				interruptID := event.Action.Interrupted.InterruptContexts[0].ID
				reInfo := event.Action.Interrupted.InterruptContexts[0].Info.([]map[string]interface{})
				if event.Output == nil {
					event.Output = &adk.AgentOutput{}
				}
				event.Output.CustomizedOutput = map[string]any{
					"interrupt_id":   interruptID,
					"interrupt_info": reInfo,
				}
				data.Output = event.Output
			}
			finalData = data

			sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
				Type:      "event",
				Data:      data,
				SessionID: sessionID,
			})

		case <-doneChan:
			// sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
			// 	Type:      "complete",
			// 	Message:   "Agent execution completed",
			// 	SessionID: sessionID,
			// })
			if finalData.Action != "interrupted" && finalData.AgentName == "视频生成助手" {
				if finalData.Output != nil {
					reInfo := make([]map[string]interface{}, 0)
					json.Unmarshal([]byte(finalData.Message), &reInfo)
					tmpAgentOutput := finalData.Output.(*adk.AgentOutput)
					tmpAgentOutput.CustomizedOutput = map[string]any{
						"interrupt_info": reInfo,
					}
					finalData.Output = tmpAgentOutput
				}
			}
			sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
				Type:      "complete",
				Data:      finalData,
				SessionID: sessionID,
			})
			return

		case <-ctx.Done():
			log.Println("Client disconnected")
			return
		}
	}
}

func sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, event AgentStreamEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal event: %v", err)
		return
	}

	// Write SSE format
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	if err != nil {
		log.Printf("Failed to write event: %v", err)
		return
	}
	flusher.Flush()
}

type AgentResumeRequest struct {
	SessionID   string `json:"session_id" binding:"required"`
	InterruptID string `json:"interrupt_id" binding:"required"`
	Input       string `json:"input" binding:"required"`
}

func (h *AgentStreamHandler) HandleAgentResume(c *gin.Context) { // ignore_security_alert IDOR
	var req AgentResumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	// Get session
	h.sessionsMu.RLock()
	session, ok := h.sessions[req.SessionID]
	h.sessionsMu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	// Set headers for SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	// Flusher to send data immediately
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	// Send connected event
	sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
		Type:      "connected",
		Message:   "Resuming agent execution",
		SessionID: req.SessionID,
	})

	// Create new event channel and done channel for this resume request
	resumeEventChan := make(chan *adk.AgentEvent, 100)
	resumeDoneChan := make(chan struct{})

	// Resume the agent
	var err error
	iter, err := session.runner.ResumeWithParams(ctx, req.SessionID, &adk.ResumeParams{
		Targets: map[string]any{
			req.InterruptID: req.Input,
		},
	})
	if err != nil {
		sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
			Type:      "error",
			Message:   "Failed to resume agent: " + err.Error(),
			SessionID: req.SessionID,
		})
		return
	}

	// Process events in a goroutine
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			event, ok := iter.Next()
			if !ok {
				break
			}

			select {
			case resumeEventChan <- event:
			case <-ctx.Done():
				log.Println("Client disconnected")
				return
			}
		}

		close(resumeDoneChan)
	}()

	defer close(resumeEventChan)

	// Process events and send to client
	var finalData eventData
	for {
		select {
		case event, ok := <-resumeEventChan:
			if !ok {
				log.Printf("Agent execution not ok, event: %+v\n", event)
				sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
					Type:      "complete",
					Message:   "Agent execution completed",
					SessionID: req.SessionID,
				})
				return
			}

			// Convert event to our data structure
			data := eventData{}
			data.AgentName = event.AgentName
			if event.Output != nil && event.Output.MessageOutput != nil {
				data.IsStreaming = event.Output.MessageOutput.IsStreaming
				if event.Output.MessageOutput.Message != nil {
					data.Message = event.Output.MessageOutput.Message.Content
				}
				data.Output = event.Output
			}
			if event.Err != nil {
				data.Err = event.Err.Error()
			}
			if event.Action != nil {
				if event.Action.Exit {
					data.Action = "exit"
				} else if event.Action.Interrupted != nil {
					data.Action = "interrupted"
				} else {
					data.Action = "normal_exec"
				}
			}
			if event.Action != nil && event.Action.Interrupted != nil && len(event.Action.Interrupted.InterruptContexts) > 0 {
				interruptID := event.Action.Interrupted.InterruptContexts[0].ID
				reInfo := event.Action.Interrupted.InterruptContexts[0].Info.([]map[string]interface{})
				if event.Output == nil {
					event.Output = &adk.AgentOutput{}
				}
				event.Output.CustomizedOutput = map[string]any{
					"interrupt_id":   interruptID,
					"interrupt_info": reInfo,
				}
				data.Output = event.Output
			}
			finalData = data

			sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
				Type:      "event",
				Data:      data,
				SessionID: req.SessionID,
			})

		case <-resumeDoneChan:
			// sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
			// 	Type:      "complete",
			// 	Message:   "Agent execution completed",
			// 	SessionID: req.SessionID,
			// })
			if finalData.Action != "interrupted" && finalData.AgentName == "视频生成助手" {
				if finalData.Output != nil {
					reInfo := make([]map[string]interface{}, 0)
					json.Unmarshal([]byte(finalData.Message), &reInfo)
					tmpAgentOutput := finalData.Output.(*adk.AgentOutput)
					tmpAgentOutput.CustomizedOutput = map[string]any{
						"interrupt_info": reInfo,
					}
					finalData.Output = tmpAgentOutput
				}
			}
			sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
				Type:      "complete",
				Data:      finalData,
				SessionID: req.SessionID,
			})
			return

		case <-ctx.Done():
			log.Println("Client disconnected")
			return

		case <-ctx.Done():
			log.Println("Resume client disconnected")
			return
		}
	}
}
