package ill_agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"illustration2/internal/model"
	"illustration2/internal/volc"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type CharacterGenerateAgent struct {
	AgentName  string
	AgentDesc  string
	ModelName  string
	ImageModel string
	ArkClient  *volc.ArkClient
}

type characterPayload struct {
	Characters []model.CharacterProfile `json:"characters"`
}

func NewCharacterGenerateAgent(ctx context.Context) adk.Agent {
	return CharacterGenerateAgent{
		AgentName:  "角色生成助手",
		AgentDesc:  characterGenerateInstruction,
		ModelName:  "ep-20260608120832-fq5kh",
		ImageModel: "ep-20251124201143-rwjnq",
		ArkClient:  volc.NewArkClientWithTimeout(180 * time.Second),
	}
}

const characterGenerateInstruction = `You are a character designer for a children's illustration video.
Read the full story and extract all recurring visual characters that need consistent appearance across chapters.

Return strict JSON only:
{
  "characters": [
    {
      "id": "char_1",
      "name": "角色名称",
      "role": "角色定位",
      "description": "中文角色设定和外观描述",
      "visual_prompt": "English visual prompt for generating a single clean character reference image, full body, consistent outfit, simple background",
      "aliases": ["别名"],
      "chapter_indices": [0, 1]
    }
  ]
}

Rules:
- chapter_indices must be zero-based.
- Include only characters that appear visually in the story.
- Keep each character appearance stable and specific: species/person, age, colors, clothing, props, silhouette, mood.
- Do not include scene descriptions in visual_prompt.
- If feedback is provided, revise the character list and visual prompts according to it.

Story theme:
%s

Chapters:
%s

Feedback:
%s`

func (r CharacterGenerateAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r CharacterGenerateAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r CharacterGenerateAgent) Run(ctx context.Context, input *adk.AgentInput, options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		if ShouldSkipStage(sessionState, StageCharacter) {
			gen.Send(StageSkippedEvent(r.AgentName))
			return
		}
		if sessionState.Story == nil || len(sessionState.Story.Chapters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("story is empty, cannot generate characters")})
			return
		}

		characters, err := r.generateCharacters(ctx, sessionState)
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		if len(characters) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("no characters generated")})
			return
		}

		for i := range characters {
			urls, err := r.ArkClient.GenerateImages(ctx, volc.ImageGenParams{
				Model:     r.ImageModel,
				Prompt:    buildCharacterReferencePrompt(characters[i]),
				Size:      "1024x1024",
				MaxImages: 1,
			})
			if err != nil {
				gen.Send(&adk.AgentEvent{Err: fmt.Errorf("character image generation failed for %s: %w", characters[i].Name, err)})
				return
			}
			characters[i].ReferenceImageURLs = urls
		}

		sessionState.Characters = characters
		sessionState.State = "character_generate"
		SaveSessionState(ctx, sessionState)

		log.Printf("characters: %+v\n", characters)
		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: "Characters generated successfully",
					},
				},
			},
		})
	}()

	return iter
}

func (r CharacterGenerateAgent) generateCharacters(ctx context.Context, sessionState *IllustrationSessionState) ([]model.CharacterProfile, error) {
	if r.ArkClient != nil && r.ArkClient.Mock {
		return mockCharacters(sessionState.Story), nil
	}

	prompt := fmt.Sprintf(
		r.AgentDesc,
		strings.TrimSpace(sessionState.Story.Theme),
		formatStoryChapters(sessionState.Story),
		strings.TrimSpace(sessionState.CharacterFeedback),
	)
	content, _, err := r.ArkClient.ChatJSONWithUsage(ctx, r.ModelName, prompt)
	if err != nil {
		return nil, fmt.Errorf("character generation failed: %w", err)
	}
	characters, err := ParseCharacterProfiles(content, len(sessionState.Story.Chapters))
	if err != nil {
		return nil, err
	}
	return characters, nil
}

func formatStoryChapters(story *model.Story) string {
	if story == nil {
		return ""
	}
	parts := make([]string, 0, len(story.Chapters))
	for i, chapter := range story.Chapters {
		parts = append(parts, fmt.Sprintf("Chapter %d: %s\n%s", i, strings.TrimSpace(chapter.Title), strings.TrimSpace(chapter.Content)))
	}
	return strings.Join(parts, "\n\n")
}

func buildCharacterReferencePrompt(character model.CharacterProfile) string {
	base := strings.TrimSpace(character.VisualPrompt)
	if base == "" {
		base = strings.TrimSpace(character.Description)
	}
	return fmt.Sprintf("%s\nSingle character reference sheet, full body, centered composition, clean simple background, consistent children's book illustration style, no text, no logo, no watermark.", base)
}

func ParseCharacterProfiles(content string, chapterCount int) ([]model.CharacterProfile, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("empty character response")
	}
	content = extractJSONObject(content)
	var payload characterPayload
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		var direct []model.CharacterProfile
		if err2 := json.Unmarshal([]byte(content), &direct); err2 != nil {
			return nil, fmt.Errorf("parse characters failed: %w", err)
		}
		payload.Characters = direct
	}
	characters := make([]model.CharacterProfile, 0, len(payload.Characters))
	for i, character := range payload.Characters {
		character.ID = strings.TrimSpace(character.ID)
		if character.ID == "" {
			character.ID = fmt.Sprintf("char_%d", i+1)
		}
		character.Name = strings.TrimSpace(character.Name)
		character.Description = strings.TrimSpace(character.Description)
		character.VisualPrompt = strings.TrimSpace(character.VisualPrompt)
		if character.Name == "" || (character.Description == "" && character.VisualPrompt == "") {
			continue
		}
		character.ChapterIndices = normalizeChapterIndices(character.ChapterIndices, chapterCount)
		characters = append(characters, character)
	}
	if len(characters) == 0 {
		return nil, errors.New("no valid characters in response")
	}
	return characters, nil
}

func extractJSONObject(content string) string {
	if strings.HasPrefix(content, "```") {
		content = strings.Trim(content, "`")
		content = strings.TrimPrefix(content, "json")
		content = strings.TrimSpace(content)
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		return content[start : end+1]
	}
	return content
}

func normalizeChapterIndices(indices []int, chapterCount int) []int {
	if chapterCount <= 0 {
		return nil
	}
	seen := make(map[int]bool)
	looksOneBased := len(indices) > 0
	for _, idx := range indices {
		if idx <= 0 || idx > chapterCount {
			looksOneBased = false
			break
		}
	}
	for _, idx := range indices {
		if looksOneBased {
			if idx >= 1 && idx <= chapterCount {
				seen[idx-1] = true
			}
		} else if idx >= 0 && idx < chapterCount {
			seen[idx] = true
		}
	}
	if len(seen) == 0 {
		for i := 0; i < chapterCount; i++ {
			seen[i] = true
		}
	}
	out := make([]int, 0, len(seen))
	for idx := range seen {
		out = append(out, idx)
	}
	sort.Ints(out)
	return out
}

func mockCharacters(story *model.Story) []model.CharacterProfile {
	name := "故事主角"
	if story != nil && len(story.Chapters) > 0 {
		if found := firstChineseNameLike(story.Chapters[0].Content); found != "" {
			name = found
		}
	}
	chapterCount := 1
	if story != nil && len(story.Chapters) > 0 {
		chapterCount = len(story.Chapters)
	}
	return []model.CharacterProfile{
		{
			ID:             "char_1",
			Name:           name,
			Role:           "主角",
			Description:    "贯穿故事的主要角色，拥有稳定、友好的儿童插画外观。",
			VisualPrompt:   "A friendly main character for a children's illustration story, consistent outfit, warm colors, full body, simple clean background",
			Aliases:        []string{name},
			ChapterIndices: normalizeChapterIndices(nil, chapterCount),
		},
	}
}

func firstChineseNameLike(text string) string {
	re := regexp.MustCompile(`[\p{Han}]{2,4}`)
	return re.FindString(text)
}
