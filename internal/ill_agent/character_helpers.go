package ill_agent

import (
	"fmt"
	"illustration2/internal/model"
	"strings"
)

func CharactersForChapter(characters []model.CharacterProfile, chapterIndex int) []model.CharacterProfile {
	matched := make([]model.CharacterProfile, 0)
	for _, character := range characters {
		if len(character.ChapterIndices) == 0 {
			matched = append(matched, character)
			continue
		}
		for _, idx := range character.ChapterIndices {
			if idx == chapterIndex {
				matched = append(matched, character)
				break
			}
		}
	}
	if len(matched) == 0 {
		return characters
	}
	return matched
}

func CharacterIDs(characters []model.CharacterProfile) []string {
	ids := make([]string, 0, len(characters))
	for _, character := range characters {
		if strings.TrimSpace(character.ID) != "" {
			ids = append(ids, character.ID)
		}
	}
	return ids
}

func FormatCharactersForPrompt(characters []model.CharacterProfile) string {
	if len(characters) == 0 {
		return "No recurring characters identified."
	}
	parts := make([]string, 0, len(characters))
	for _, character := range characters {
		parts = append(parts, fmt.Sprintf(
			"- %s (%s): %s Visual consistency prompt: %s",
			strings.TrimSpace(character.Name),
			strings.TrimSpace(character.Role),
			strings.TrimSpace(character.Description),
			strings.TrimSpace(character.VisualPrompt),
		))
	}
	return strings.Join(parts, "\n")
}

func ReferenceImagesForCharacterIDs(characters []model.CharacterProfile, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		allowed[strings.TrimSpace(id)] = true
	}
	selected := make([]model.CharacterProfile, 0, len(ids))
	for _, character := range characters {
		if allowed[strings.TrimSpace(character.ID)] {
			selected = append(selected, character)
		}
	}
	return ReferenceImagesForCharacters(selected)
}

func ReferenceImagesForCharacters(characters []model.CharacterProfile) []string {
	seen := make(map[string]bool)
	urls := make([]string, 0)
	for _, character := range characters {
		for _, url := range character.ReferenceImageURLs {
			url = strings.TrimSpace(url)
			if url == "" || seen[url] {
				continue
			}
			seen[url] = true
			urls = append(urls, url)
		}
	}
	return urls
}
