package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"illustration2/internal/auth"
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
	store      *auth.Store
	sessions   map[string]*agentSession
	sessionsMu sync.RWMutex
}

type agentSession struct {
	runner *adk.Runner
	userID string
	cancel context.CancelFunc
}

func NewAgentStreamHandler(genService *service.GenerationService, store *auth.Store) *AgentStreamHandler {
	return &AgentStreamHandler{
		genService: genService,
		store:      store,
		sessions:   make(map[string]*agentSession),
	}
}

func (h *AgentStreamHandler) createAgentWork(userID, theme string) (string, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		sessionID := uuid.New().String()
		if _, err := h.store.CreateAgentWork(userID, sessionID, theme); err != nil {
			lastErr = err
			continue
		}
		return sessionID, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("failed to create agent work")
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
	user, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req AgentStreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	theme := req.Theme
	if theme == "" {
		theme = "恐龙为什么灭绝了？"
	}

	sessionID, err := h.createAgentWork(user.ID, theme)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Set headers for SSE
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a context that will be canceled if client disconnects
	ctx, cancel := context.WithCancel(c.Request.Context())
	ctx = ill_agent.WithAgentContext(ctx, sessionID, user.ID, h.store)

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
		userID: user.ID,
		cancel: cancel,
	}
	h.sessionsMu.Lock()
	h.sessions[sessionID] = session
	h.sessionsMu.Unlock()
	_ = h.store.AddHistory(user.ID, auth.GenerationHistory{
		Kind:      "agent_stream",
		ModelName: "illustration-agent",
		Prompt:    theme,
		Status:    "processing",
		TaskID:    sessionID,
		Summary:   "illustration agent session started",
	})

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
				ill_agent.SyncAgentWork(ctx, "failed", data.Err)
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
				ill_agent.SyncAgentWork(ctx, "waiting_feedback", "")
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
			if finalData.Action != "interrupted" && (finalData.AgentName == "视频生成助手" || finalData.AgentName == "章节视频生成助手") {
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
			if finalData.Action == "interrupted" {
				ill_agent.SyncAgentWork(ctx, "waiting_feedback", "")
			} else if finalData.Err != "" {
				ill_agent.SyncAgentWork(ctx, "failed", finalData.Err)
			} else {
				ill_agent.SyncAgentWork(ctx, "succeeded", "")
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

type AgentRecoverRequest struct {
	SessionID string `json:"session_id" binding:"required"`
	Input     string `json:"input" binding:"required"`
}

type AgentStopRequest struct {
	SessionID string `json:"session_id" binding:"required"`
}

func (h *AgentStreamHandler) HandleAgentResume(c *gin.Context) { // ignore_security_alert IDOR
	user, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req AgentResumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	ctx = ill_agent.WithAgentContext(ctx, req.SessionID, user.ID, h.store)

	// Get session
	h.sessionsMu.RLock()
	session, ok := h.sessions[req.SessionID]
	h.sessionsMu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if session.userID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "session does not belong to current user"})
		return
	}

	if plan := ill_agent.InferRestartPlan(ctx, req.Input, ill_agent.GetSessionState(ctx), false); plan.ShouldRestart {
		ill_agent.ApplyRestartPlan(ill_agent.GetSessionState(ctx), plan)
		ill_agent.SaveSessionState(ctx, ill_agent.GetSessionState(ctx))
		_ = h.store.AddHistory(user.ID, auth.GenerationHistory{
			Kind:      "agent_resume",
			ModelName: "illustration-agent",
			Prompt:    req.Input,
			Status:    "processing",
			TaskID:    req.SessionID,
			Summary:   "illustration agent restarted from " + plan.Stage,
		})
		a := ill_agent.NewMKAgent(ctx)
		runner := adk.NewRunner(ctx, adk.RunnerConfig{
			EnableStreaming: true,
			Agent:           a,
			CheckPointStore: store.NewInMemoryStore(),
		})
		h.sessionsMu.Lock()
		if old := h.sessions[req.SessionID]; old != nil && old.cancel != nil {
			old.cancel()
		}
		h.sessions[req.SessionID] = &agentSession{runner: runner, userID: user.ID, cancel: cancel}
		h.sessionsMu.Unlock()
		iter := runner.Query(ctx, req.Input, adk.WithCheckPointID(req.SessionID))
		h.streamAgentIterator(c, ctx, req.SessionID, "Restarting agent from "+plan.Stage, iter)
		return
	}

	ill_agent.SyncAgentWork(ctx, "processing", "")
	h.sessionsMu.Lock()
	if current := h.sessions[req.SessionID]; current != nil {
		current.cancel = cancel
	}
	h.sessionsMu.Unlock()
	_ = h.store.AddHistory(user.ID, auth.GenerationHistory{
		Kind:      "agent_resume",
		ModelName: "illustration-agent",
		Prompt:    req.Input,
		Status:    "processing",
		TaskID:    req.SessionID,
		Summary:   "illustration agent resumed",
	})

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
		ill_agent.SyncAgentWork(ctx, "failed", err.Error())
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
				ill_agent.SyncAgentWork(ctx, "failed", data.Err)
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
				ill_agent.SyncAgentWork(ctx, "waiting_feedback", "")
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
			if finalData.Action != "interrupted" && (finalData.AgentName == "视频生成助手" || finalData.AgentName == "章节视频生成助手") {
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
			if finalData.Action == "interrupted" {
				ill_agent.SyncAgentWork(ctx, "waiting_feedback", "")
			} else if finalData.Err != "" {
				ill_agent.SyncAgentWork(ctx, "failed", finalData.Err)
			} else {
				ill_agent.SyncAgentWork(ctx, "succeeded", "")
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
		}
	}
}

func (h *AgentStreamHandler) HandleAgentStop(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req AgentStopRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	h.sessionsMu.RLock()
	session, ok := h.sessions[req.SessionID]
	h.sessionsMu.RUnlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	if session.userID != user.ID {
		c.JSON(http.StatusForbidden, gin.H{"error": "session does not belong to current user"})
		return
	}

	if session.cancel != nil {
		session.cancel()
	}
	ctx := ill_agent.WithAgentContext(c.Request.Context(), req.SessionID, user.ID, h.store)
	ill_agent.SyncAgentWork(ctx, "canceled", "用户手动停止流程")
	_ = h.store.AddHistory(user.ID, auth.GenerationHistory{
		Kind:      "agent_stop",
		ModelName: "illustration-agent",
		Prompt:    "stop",
		Status:    "canceled",
		TaskID:    req.SessionID,
		Summary:   "illustration agent stopped by user",
	})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *AgentStreamHandler) HandleAgentRecover(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req AgentRecoverRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	work, err := h.store.GetAgentWork(user.ID, req.SessionID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithCancel(c.Request.Context())
	ctx = ill_agent.WithAgentContext(ctx, req.SessionID, user.ID, h.store)
	state := ill_agent.SessionStateFromAgentWork(work)
	plan := ill_agent.InferRestartPlan(ctx, req.Input, state, true)
	ill_agent.ApplyRestartPlan(state, plan)
	ill_agent.SaveSessionState(ctx, state)

	_ = h.store.AddHistory(user.ID, auth.GenerationHistory{
		Kind:      "agent_resume",
		ModelName: "illustration-agent",
		Prompt:    req.Input,
		Status:    "processing",
		TaskID:    req.SessionID,
		Summary:   "illustration agent recovered from history at " + plan.Stage,
	})

	a := ill_agent.NewMKAgent(ctx)
	runner := adk.NewRunner(ctx, adk.RunnerConfig{
		EnableStreaming: true,
		Agent:           a,
		CheckPointStore: store.NewInMemoryStore(),
	})
	h.sessionsMu.Lock()
	if old := h.sessions[req.SessionID]; old != nil && old.cancel != nil {
		old.cancel()
	}
	h.sessions[req.SessionID] = &agentSession{runner: runner, userID: user.ID, cancel: cancel}
	h.sessionsMu.Unlock()

	iter := runner.Query(ctx, req.Input, adk.WithCheckPointID(req.SessionID))
	h.streamAgentIterator(c, ctx, req.SessionID, "Recovering agent work from "+plan.Stage, iter)
}

func (h *AgentStreamHandler) streamAgentIterator(c *gin.Context, ctx context.Context, sessionID, connectedMessage string, iter *adk.AsyncIterator[*adk.AgentEvent]) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
		Type:      "connected",
		Message:   connectedMessage,
		SessionID: sessionID,
	})

	eventChan := make(chan *adk.AgentEvent, 100)
	doneChan := make(chan struct{})
	go func() {
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
			data := eventData{AgentName: event.AgentName}
			if event.Output != nil && event.Output.MessageOutput != nil {
				data.IsStreaming = event.Output.MessageOutput.IsStreaming
				if event.Output.MessageOutput.Message != nil {
					data.Message = event.Output.MessageOutput.Message.Content
				}
				data.Output = event.Output
			}
			if event.Err != nil {
				data.Err = event.Err.Error()
				ill_agent.SyncAgentWork(ctx, "failed", data.Err)
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
				reInfo, _ := event.Action.Interrupted.InterruptContexts[0].Info.([]map[string]interface{})
				if event.Output == nil {
					event.Output = &adk.AgentOutput{}
				}
				event.Output.CustomizedOutput = map[string]any{
					"interrupt_id":   interruptID,
					"interrupt_info": reInfo,
				}
				data.Output = event.Output
				ill_agent.SyncAgentWork(ctx, "waiting_feedback", "")
			}
			finalData = data
			sendSSEEvent(c.Writer, flusher, AgentStreamEvent{
				Type:      "event",
				Data:      data,
				SessionID: sessionID,
			})
		case <-doneChan:
			if finalData.Action != "interrupted" && (finalData.AgentName == "视频生成助手" || finalData.AgentName == "章节视频生成助手") {
				if finalData.Output != nil {
					reInfo := make([]map[string]interface{}, 0)
					json.Unmarshal([]byte(finalData.Message), &reInfo)
					if tmpAgentOutput, ok := finalData.Output.(*adk.AgentOutput); ok {
						tmpAgentOutput.CustomizedOutput = map[string]any{
							"interrupt_info": reInfo,
						}
						finalData.Output = tmpAgentOutput
					}
				}
			}
			if finalData.Action == "interrupted" {
				ill_agent.SyncAgentWork(ctx, "waiting_feedback", "")
			} else if finalData.Err != "" {
				ill_agent.SyncAgentWork(ctx, "failed", finalData.Err)
			} else {
				ill_agent.SyncAgentWork(ctx, "succeeded", "")
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
