package ill_agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"illustration2/internal/tools"
	"illustration2/internal/volc"
)

type Chapter struct {
	ID           int    `json:"id"`
	ChapterTitle string `json:"chapter_title"`
	ChapterBody  string `json:"chapter_body"`
	ImagePrompt  string `json:"image_prompt,omitempty"`
	ImageURL     string `json:"image_url,omitempty"`
}

type Story struct {
	Chapters []*Chapter `json:"chapters"`
}

const (
	StateInit                = "init"
	StateStoryWaitingReview  = "story_waiting_review"
	StatePromptsGenerated    = "prompts_generated"
	StateImagesWaitingReview = "images_waiting_review"
	StateVideoGenerated      = "video_generated"
	CmdNoNeedToEdit          = "no need to edit"
)

const storyWriterInstruction = `You are a children's story writer. Generate a 3-chapter story based on the user's theme. Respond in valid JSON format: {"chapters": [{"chapter_title": "...", "chapter_body": "..."}]}`
const promptEngineerInstruction = `You are a prompt engineer. For each chapter, add a detailed "image_prompt". Respond in valid JSON.`

type SessionState struct {
	State    string `json:"state"`
	Theme    string `json:"theme"`
	Story    *Story `json:"story"`
	VideoURL string `json:"video_url"`
}

type IllustrationAgent struct {
	chatModel *ark.ChatModel
	imageTool tool.InvokableTool
	videoTool tool.InvokableTool
}

func NewIllustrationAgent(ctx context.Context) (*IllustrationAgent, error) {
	arkClient := volc.NewArkClientDefault()
	chatModel, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		APIKey:     arkClient.APIKey,
		HTTPClient: arkClient.HTTPClient,
		Model:      "ep-20250220181854-c8s82",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create chat model: %w", err)
	}

	return &IllustrationAgent{
		chatModel: chatModel,
		imageTool: tools.NewImageTool(arkClient),
		videoTool: tools.NewVideoTool(arkClient),
	}, nil
}

func (a *IllustrationAgent) Invoke(ctx context.Context, state *SessionState, input string) (*SessionState, any, error) {
	switch state.State {
	case StateInit:
		return a.handleStoryGeneration(ctx, state, input)
	case StateStoryWaitingReview:
		if strings.ToLower(input) == CmdNoNeedToEdit {
			return a.handlePromptGeneration(ctx, state)
		}
		return a.handleStoryRevision(ctx, state, input)
	case StatePromptsGenerated:
		return a.handleImageGeneration(ctx, state)
	case StateImagesWaitingReview:
		if strings.ToLower(input) == CmdNoNeedToEdit {
			return a.handleVideoGeneration(ctx, state)
		}
		return a.handleImageRevision(ctx, state, input)
	case StateVideoGenerated:
		return state, "The video has already been generated.", nil
	default:
		return state, nil, fmt.Errorf("unknown state: %s", state.State)
	}
}

// State Handlers
func (a *IllustrationAgent) handleStoryGeneration(ctx context.Context, state *SessionState, theme string) (*SessionState, any, error) {
	log.Println("State: Generating story for theme:", theme)
	state.Theme = theme
	content, err := a.runLLM(ctx, storyWriterInstruction, fmt.Sprintf("Theme: %s", theme))
	if err != nil { return state, nil, err }
	story, err := a.parseStory(content)
	if err != nil { return state, nil, err }
	state.Story = story
	state.State = StateStoryWaitingReview
	return state, story, nil
}

func (a *IllustrationAgent) handleStoryRevision(ctx context.Context, state *SessionState, feedback string) (*SessionState, any, error) {
	log.Println("State: Revising story with feedback:", feedback)
	storyBytes, _ := json.Marshal(state.Story)
	prompt := fmt.Sprintf("Revise the following story based on this feedback: '%s'.\n\nStory:\n%s", feedback, string(storyBytes))
	content, err := a.runLLM(ctx, storyWriterInstruction, prompt)
	if err != nil { return state, nil, err }
	story, err := a.parseStory(content)
	if err != nil { return state, nil, err }
	state.Story = story
	state.State = StateStoryWaitingReview
	return state, story, nil
}

func (a *IllustrationAgent) handlePromptGeneration(ctx context.Context, state *SessionState) (*SessionState, any, error) {
	log.Println("State: Generating prompts...")
	storyBytes, _ := json.Marshal(state.Story)
	content, err := a.runLLM(ctx, promptEngineerInstruction, string(storyBytes))
	if err != nil { return state, nil, err }
	storyWithPrompts, err := a.parseStory(content)
	if err != nil { return state, nil, err }
	state.Story = storyWithPrompts
	state.State = StatePromptsGenerated
	return a.handleImageGeneration(ctx, state)
}

func (a *IllustrationAgent) handleImageGeneration(ctx context.Context, state *SessionState) (*SessionState, any, error) {
	log.Println("State: Generating images...")
	for _, chapter := range state.Story.Chapters {
		toolInput := fmt.Sprintf(`{"prompt": "%s"}`, chapter.ImagePrompt)
		output, err := a.imageTool.InvokableRun(ctx, toolInput)
		if err != nil { return state, nil, err }
		var resp tools.ImageToolResp
		if err := json.Unmarshal([]byte(output), &resp); err == nil && len(resp.Images) > 0 {
			chapter.ImageURL = resp.Images[0]
		} else { return state, nil, fmt.Errorf("failed to parse image tool output: %s", output) }
	}
	state.State = StateImagesWaitingReview
	return state, state.Story, nil
}

func (a *IllustrationAgent) handleImageRevision(ctx context.Context, state *SessionState, feedback string) (*SessionState, any, error) {
	log.Println("State: Revising images...")
	for _, chapter := range state.Story.Chapters {
		newPrompt := fmt.Sprintf("%s. User feedback: %s", chapter.ImagePrompt, feedback)
		toolInput := fmt.Sprintf(`{"prompt": "%s"}`, newPrompt)
		output, err := a.imageTool.InvokableRun(ctx, toolInput)
		if err != nil { return state, nil, err }
		var resp tools.ImageToolResp
		if err := json.Unmarshal([]byte(output), &resp); err == nil && len(resp.Images) > 0 {
			chapter.ImageURL = resp.Images[0]
		} else { return state, nil, fmt.Errorf("failed to parse revised image tool output: %s", output) }
	}
	state.State = StateImagesWaitingReview
	return state, state.Story, nil
}

func (a *IllustrationAgent) handleVideoGeneration(ctx context.Context, state *SessionState) (*SessionState, any, error) {
	log.Println("State: Generating video...")
	var refImages []string
	for _, ch := range state.Story.Chapters { refImages = append(refImages, ch.ImageURL) }
	videoArgs := tools.VideoToolArgs{Prompt: state.Theme, ReferenceImageURLs: refImages}
	argsBytes, _ := json.Marshal(videoArgs)
	output, err := a.videoTool.InvokableRun(ctx, string(argsBytes))
	if err != nil { return state, nil, err }
	var resp tools.VideoToolResp
	if err := json.Unmarshal([]byte(output), &resp); err == nil {
		state.VideoURL = resp.VideoURL
	} else { return state, nil, fmt.Errorf("failed to parse video tool output: %s", output) }
	state.State = StateVideoGenerated
	return state, state.VideoURL, nil
}

// Helpers
func (a *IllustrationAgent) runLLM(ctx context.Context, instruction, userPrompt string) (string, error) {
	plainChatModel, err := a.chatModel.WithTools(nil)
	if err != nil {
		return "", fmt.Errorf("failed to prepare chat model: %w", err)
	}

	graph := compose.NewGraph[[]*schema.Message, *schema.Message]()
	graph.AddChatModelNode("model", plainChatModel)
	graph.AddEdge(compose.START, "model")
	gAgent, err := graph.Compile(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to compile graph: %w", err)
	}

	messages := []*schema.Message{{Role: schema.System, Content: instruction}, {Role: schema.User, Content: userPrompt}}
	res, err := gAgent.Invoke(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("graph invocation failed: %w", err)
	}
	return res.Content, nil
}

func (a *IllustrationAgent) parseStory(content string) (*Story, error) {
	cleanedContent := strings.TrimSpace(content)
	if strings.HasPrefix(cleanedContent, "```json") {
		cleanedContent = strings.TrimPrefix(cleanedContent, "```json")
		cleanedContent = strings.TrimSuffix(cleanedContent, "```")
	}
	var story Story
	if err := json.Unmarshal([]byte(cleanedContent), &story); err != nil {
		return nil, fmt.Errorf("failed to unmarshal story: %w, raw: %s", err, cleanedContent)
	}
	for i := range story.Chapters {
		story.Chapters[i].ID = i + 1
	}
	return &story, nil
}