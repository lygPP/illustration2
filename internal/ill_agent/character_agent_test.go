package ill_agent

import (
	"context"
	"illustration2/internal/auth"
	"illustration2/internal/model"
	"illustration2/internal/volc"
	"path/filepath"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestParseCharacterProfilesNormalizesChapters(t *testing.T) {
	content := `{
		"characters": [
			{
				"id": "hero",
				"name": "小星",
				"role": "主角",
				"description": "穿红色斗篷的小女孩",
				"visual_prompt": "A little girl in a red cape",
				"aliases": ["小星"],
				"chapter_indices": [1, 3]
			}
		]
	}`
	characters, err := ParseCharacterProfiles(content, 3)
	if err != nil {
		t.Fatalf("ParseCharacterProfiles() error = %v", err)
	}
	if len(characters) != 1 {
		t.Fatalf("characters length = %d", len(characters))
	}
	got := characters[0].ChapterIndices
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("chapter indices = %+v", got)
	}
}

func TestCharactersForChapterAndReferenceImages(t *testing.T) {
	characters := []model.CharacterProfile{
		{
			ID:                 "hero",
			Name:               "小星",
			ChapterIndices:     []int{0, 1},
			ReferenceImageURLs: []string{"hero.png"},
		},
		{
			ID:                 "cat",
			Name:               "云猫",
			ChapterIndices:     []int{1},
			ReferenceImageURLs: []string{"cat.png", "hero.png"},
		},
	}
	chapterZero := CharactersForChapter(characters, 0)
	if len(chapterZero) != 1 || chapterZero[0].ID != "hero" {
		t.Fatalf("chapter 0 characters = %+v", chapterZero)
	}
	refs := ReferenceImagesForCharacterIDs(characters, []string{"hero", "cat"})
	if len(refs) != 2 || refs[0] != "hero.png" || refs[1] != "cat.png" {
		t.Fatalf("refs = %+v", refs)
	}
}

func TestMockCharacters(t *testing.T) {
	story := &model.Story{
		Theme: "森林冒险",
		Chapters: []model.StoryChapter{
			{Title: "第一章", Content: "小鹿米米走进森林。"},
			{Title: "第二章", Content: "米米遇见了星星。"},
		},
	}
	characters := mockCharacters(story)
	if len(characters) == 0 {
		t.Fatal("mockCharacters() returned no characters")
	}
	if len(characters[0].ChapterIndices) != 2 {
		t.Fatalf("mock character chapter indices = %+v", characters[0].ChapterIndices)
	}
}

func TestStoryGenerateAgentRecordsModelUsage(t *testing.T) {
	store, err := auth.NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	user, _, err := store.Register("story-user", "secret123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx := WithAgentContext(context.Background(), "story-usage-test", user.ID, store)
	agent := &StoryGenerateAgent{
		AgentName: "故事生成助手",
		AgentDesc: storyGenerateInstruction,
		ModelName: storyModelName,
		ArkClient: &volc.ArkClient{Mock: true},
	}

	iter := agent.Run(ctx, &adk.AgentInput{Messages: []adk.Message{schema.UserMessage("森林冒险")}})
	event, ok := iter.Next()
	if !ok {
		t.Fatal("story agent produced no event")
	}
	if event.Err != nil {
		t.Fatalf("story agent error = %v", event.Err)
	}

	stats := store.ListUsage(user.ID)
	byModel := make(map[string]auth.UsageStat, len(stats))
	for _, stat := range stats {
		byModel[stat.ModelName] = stat
	}
	if byModel[storyModelName].RequestCount != 1 {
		t.Fatalf("story model usage = %+v", byModel[storyModelName])
	}
	if _, ok := byModel["illustration-agent"]; ok {
		t.Fatalf("unexpected illustration-agent usage = %+v", byModel["illustration-agent"])
	}
}
