package ill_agent

import (
	"illustration2/internal/model"
	"testing"
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
