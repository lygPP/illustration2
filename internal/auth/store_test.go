package auth

import (
	"illustration2/internal/model"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store
}

func TestEnsureSuperAdminCreatesLoginableAdmin(t *testing.T) {
	t.Setenv("SUPER_ADMIN_USERNAME", "root")
	t.Setenv("SUPER_ADMIN_PASSWORD", "secret123")
	store := newTestStore(t)

	if err := store.EnsureSuperAdmin(); err != nil {
		t.Fatalf("EnsureSuperAdmin() error = %v", err)
	}
	user, token, err := store.Login("root", "secret123")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if token == "" {
		t.Fatal("Login() token is empty")
	}
	if user.Role != RoleSuperAdmin || user.Status != StatusActive {
		t.Fatalf("Login() user role/status = %s/%s", user.Role, user.Status)
	}
}

func TestDisabledUserCannotLoginOrUseToken(t *testing.T) {
	store := newTestStore(t)
	user, token, err := store.Register("alice", "secret123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := store.AdminUpdateUser(user.ID, AdminUserInput{
		Nickname:  user.Nickname,
		AvatarURL: user.AvatarURL,
		Role:      RoleUser,
		Status:    StatusDisabled,
	}); err != nil {
		t.Fatalf("AdminUpdateUser() error = %v", err)
	}
	if _, _, err := store.Login("alice", "secret123"); err == nil {
		t.Fatal("Login() expected disabled user error")
	}
	if _, err := store.UserByToken(token); err == nil {
		t.Fatal("UserByToken() expected disabled user error")
	}
}

func TestAdminUserManagementAndSoftDelete(t *testing.T) {
	store := newTestStore(t)
	user, err := store.AdminCreateUser(AdminUserInput{
		Username: "bob",
		Password: "secret123",
		Nickname: "Bob",
		Role:     RoleUser,
		Status:   StatusActive,
	})
	if err != nil {
		t.Fatalf("AdminCreateUser() error = %v", err)
	}
	if len(store.ListUsers("bob", "", "")) != 1 {
		t.Fatal("ListUsers() did not find created user")
	}
	if err := store.AdminResetPassword(user.ID, "newsecret"); err != nil {
		t.Fatalf("AdminResetPassword() error = %v", err)
	}
	if _, _, err := store.Login("bob", "newsecret"); err != nil {
		t.Fatalf("Login() with reset password error = %v", err)
	}
	if err := store.AdminSoftDeleteUser(user.ID); err != nil {
		t.Fatalf("AdminSoftDeleteUser() error = %v", err)
	}
	detail, err := store.AdminGetUser(user.ID)
	if err != nil {
		t.Fatalf("AdminGetUser() error = %v", err)
	}
	if detail.User.Status != StatusDeleted {
		t.Fatalf("deleted user status = %s", detail.User.Status)
	}
}

func TestPersonaAndVoiceLifecycle(t *testing.T) {
	store := newTestStore(t)
	user, _, err := store.Register("creator", "secret123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	persona, err := store.CreatePersona(user.ID, PersonaInput{Name: "小画家", Description: "红色围巾"})
	if err != nil {
		t.Fatalf("CreatePersona() error = %v", err)
	}
	persona, err = store.UpdatePersonaImage(user.ID, persona.ID, "/uploads/personas/creator/ref.png")
	if err != nil {
		t.Fatalf("UpdatePersonaImage() error = %v", err)
	}
	if persona.ImageURL == "" || len(store.ListPersonas(user.ID)) != 1 {
		t.Fatal("persona image/list not updated")
	}

	voice, err := store.CreateVoice(user.ID, VoiceInput{Name: "旁白"})
	if err != nil {
		t.Fatalf("CreateVoice() error = %v", err)
	}
	voice, err = store.UpdateVoiceSample(user.ID, voice.ID, "/uploads/voices/creator/sample.wav")
	if err != nil {
		t.Fatalf("UpdateVoiceSample() error = %v", err)
	}
	voice, err = store.ApplyVoiceCloneResult(user.ID, voice.ID, VoiceCloneResult{
		GeneratedVoiceID: "mock_voice",
		VoiceType:        "mock_voice",
		Status:           "ready",
	})
	if err != nil {
		t.Fatalf("ApplyVoiceCloneResult() error = %v", err)
	}
	if voice.Status != "ready" || voice.VoiceType == "" {
		t.Fatalf("voice status/type = %s/%s", voice.Status, voice.VoiceType)
	}
}

func TestUsageStatsAggregateByModel(t *testing.T) {
	store := newTestStore(t)
	user, _, err := store.Register("usage-user", "secret123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if err := store.AddUsage(user.ID, "chat-model", 10, 4); err != nil {
		t.Fatalf("AddUsage(chat 1) error = %v", err)
	}
	if err := store.AddUsage(user.ID, "chat-model", 2, 1); err != nil {
		t.Fatalf("AddUsage(chat 2) error = %v", err)
	}
	if err := store.AddUsage(user.ID, "image-model", 0, 0); err != nil {
		t.Fatalf("AddUsage(image) error = %v", err)
	}

	stats := store.ListUsage(user.ID)
	byModel := make(map[string]UsageStat, len(stats))
	for _, stat := range stats {
		byModel[stat.ModelName] = stat
	}

	chat := byModel["chat-model"]
	if chat.RequestCount != 2 || chat.PromptTokens != 12 || chat.CompletionTokens != 5 || chat.TotalTokens != 17 {
		t.Fatalf("chat usage = %+v", chat)
	}
	image := byModel["image-model"]
	if image.RequestCount != 1 || image.TotalTokens != 0 {
		t.Fatalf("image usage = %+v", image)
	}
}

func TestAgentWorkLifecycleAndMergedHistory(t *testing.T) {
	store := newTestStore(t)
	user, _, err := store.Register("agent-user", "secret123")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	other, _, err := store.Register("other-user", "secret123")
	if err != nil {
		t.Fatalf("Register(other) error = %v", err)
	}

	if _, err := store.CreateAgentWork(user.ID, "session-1", "森林冒险"); err != nil {
		t.Fatalf("CreateAgentWork() error = %v", err)
	}
	if _, err := store.CreateAgentWork(user.ID, "session-1", "重复任务"); err == nil {
		t.Fatal("CreateAgentWork() expected duplicate error")
	}
	if err := store.UpdateAgentWorkSnapshot(user.ID, "session-1", "succeeded", "", AgentWorkSnapshot{
		Story: &model.Story{
			Theme: "森林冒险",
			Chapters: []model.StoryChapter{
				{Title: "第一章", Content: "小鹿出发。"},
			},
		},
		Characters: []model.CharacterProfile{
			{ID: "hero", Name: "小鹿", Description: "勇敢的小鹿", ReferenceImageURLs: []string{"/resource/hero.png"}},
		},
		ConfirmedImages:          map[int][]string{0: []string{"/resource/chapter.png"}},
		ChapterAudioURLs:         map[int]string{0: "/resource/chapter.mp3"},
		NarratedChapterVideoURLs: map[int]string{0: "/resource/chapter.mp4"},
		VideoURL:                 "/resource/final.mp4",
	}); err != nil {
		t.Fatalf("UpdateAgentWorkSnapshot() error = %v", err)
	}

	work, err := store.GetAgentWork(user.ID, "session-1")
	if err != nil {
		t.Fatalf("GetAgentWork() error = %v", err)
	}
	if work.Status != "succeeded" || work.VideoURL == "" || work.PreviewURL != "/resource/final.mp4" {
		t.Fatalf("work = %+v", work)
	}
	if _, err := store.GetAgentWork(other.ID, "session-1"); err == nil {
		t.Fatal("GetAgentWork(other) expected not found")
	}

	items := store.ListMergedHistory(user.ID)
	if len(items) != 1 {
		t.Fatalf("merged history length = %d", len(items))
	}
	if items[0].ItemType != "agent_work" || items[0].SessionID != "session-1" || items[0].Prompt != "森林冒险" {
		t.Fatalf("merged item = %+v", items[0])
	}
}
