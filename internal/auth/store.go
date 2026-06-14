package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"illustration2/internal/model"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	RoleUser       = "user"
	RoleSuperAdmin = "super_admin"

	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusDeleted  = "deleted"
)

type Store struct {
	mu    sync.RWMutex
	path  string
	state appState
}

type appState struct {
	Users      map[string]*User                 `json:"users"`
	Sessions   map[string]*Session              `json:"sessions"`
	Histories  map[string][]GenerationHistory   `json:"histories"`
	Usage      map[string]map[string]*UsageStat `json:"usage"`
	Personas   map[string][]Persona             `json:"personas"`
	Voices     map[string][]VoiceProfile        `json:"voices"`
	AgentWorks map[string]map[string]*AgentWork `json:"agent_works"`
}

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Nickname     string    `json:"nickname,omitempty"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	PasswordHash string    `json:"password_hash"`
	PasswordSalt string    `json:"password_salt"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	DeletedAt    time.Time `json:"deleted_at,omitempty"`
}

type PublicUser struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Nickname  string    `json:"nickname,omitempty"`
	AvatarURL string    `json:"avatar_url,omitempty"`
	Role      string    `json:"role"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
}

type GenerationHistory struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	Kind         string    `json:"kind"`
	ResourceType string    `json:"resource_type,omitempty"`
	ModelName    string    `json:"model_name"`
	Prompt       string    `json:"prompt"`
	Status       string    `json:"status"`
	TaskID       string    `json:"task_id,omitempty"`
	PreviewURL   string    `json:"preview_url,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type AgentWork struct {
	ID                       string                   `json:"id"`
	SessionID                string                   `json:"session_id"`
	UserID                   string                   `json:"user_id"`
	Kind                     string                   `json:"kind"`
	ResourceType             string                   `json:"resource_type"`
	ModelName                string                   `json:"model_name"`
	Theme                    string                   `json:"theme"`
	Status                   string                   `json:"status"`
	ErrorMessage             string                   `json:"error_message,omitempty"`
	State                    string                   `json:"state,omitempty"`
	Story                    *model.Story             `json:"story,omitempty"`
	Characters               []model.CharacterProfile `json:"characters,omitempty"`
	ImagePrompts             []model.ImagePrompt      `json:"image_prompts,omitempty"`
	GeneratedImages          map[int][]string         `json:"generated_images,omitempty"`
	ConfirmedImages          map[int][]string         `json:"confirmed_images,omitempty"`
	CurrentImageChapter      int                      `json:"current_image_chapter,omitempty"`
	VideoPrompt              string                   `json:"video_prompt,omitempty"`
	ChapterVideoPrompts      []model.VideoPrompt      `json:"chapter_video_prompts,omitempty"`
	ChapterVideoURLs         map[int]string           `json:"chapter_video_urls,omitempty"`
	ChapterAudioURLs         map[int]string           `json:"chapter_audio_urls,omitempty"`
	NarratedChapterVideoURLs map[int]string           `json:"narrated_chapter_video_urls,omitempty"`
	VideoURL                 string                   `json:"video_url,omitempty"`
	SelectedVoiceID          string                   `json:"selected_voice_id,omitempty"`
	SelectedVoiceType        string                   `json:"selected_voice_type,omitempty"`
	SelectedVoiceName        string                   `json:"selected_voice_name,omitempty"`
	PreviewURL               string                   `json:"preview_url,omitempty"`
	Summary                  string                   `json:"summary,omitempty"`
	CreatedAt                time.Time                `json:"created_at"`
	UpdatedAt                time.Time                `json:"updated_at"`
}

type AgentWorkSnapshot struct {
	State                    string
	Story                    *model.Story
	Characters               []model.CharacterProfile
	ImagePrompts             []model.ImagePrompt
	GeneratedImages          map[int][]string
	ConfirmedImages          map[int][]string
	CurrentImageChapter      int
	VideoPrompt              string
	ChapterVideoPrompts      []model.VideoPrompt
	ChapterVideoURLs         map[int]string
	ChapterAudioURLs         map[int]string
	NarratedChapterVideoURLs map[int]string
	VideoURL                 string
	SelectedVoiceID          string
	SelectedVoiceType        string
	SelectedVoiceName        string
}

type HistoryItem struct {
	ItemType     string    `json:"item_type"`
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id,omitempty"`
	UserID       string    `json:"user_id"`
	Kind         string    `json:"kind"`
	ResourceType string    `json:"resource_type,omitempty"`
	ModelName    string    `json:"model_name"`
	Prompt       string    `json:"prompt"`
	Theme        string    `json:"theme,omitempty"`
	Status       string    `json:"status"`
	TaskID       string    `json:"task_id,omitempty"`
	PreviewURL   string    `json:"preview_url,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at,omitempty"`
}

type UsageStat struct {
	ModelName        string    `json:"model_name"`
	RequestCount     int       `json:"request_count"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Persona struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	ImageURL    string    `json:"image_url,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type VoiceProfile struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	Name             string    `json:"name"`
	Description      string    `json:"description,omitempty"`
	Status           string    `json:"status"`
	SampleAudioURL   string    `json:"sample_audio_url,omitempty"`
	ArkTaskID        string    `json:"ark_task_id,omitempty"`
	GeneratedVoiceID string    `json:"generated_voice_id,omitempty"`
	VoiceType        string    `json:"voice_type,omitempty"`
	PreviewAudioURL  string    `json:"preview_audio_url,omitempty"`
	ErrorMessage     string    `json:"error_message,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func NewStore(path string) (*Store, error) {
	if path == "" {
		path = os.Getenv("APP_DATA_PATH")
	}
	if path == "" {
		path = filepath.Join("data", "app_state.json")
	}
	s := &Store{path: path}
	s.state = newAppState()
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func newAppState() appState {
	return appState{
		Users:      make(map[string]*User),
		Sessions:   make(map[string]*Session),
		Histories:  make(map[string][]GenerationHistory),
		Usage:      make(map[string]map[string]*UsageStat),
		Personas:   make(map[string][]Persona),
		Voices:     make(map[string][]VoiceProfile),
		AgentWorks: make(map[string]map[string]*AgentWork),
	}
}

func (s *Store) EnsureSuperAdmin() error {
	username := strings.TrimSpace(os.Getenv("SUPER_ADMIN_USERNAME"))
	if username == "" {
		username = "superadmin"
	}
	password := os.Getenv("SUPER_ADMIN_PASSWORD")
	generated := false
	if strings.TrimSpace(password) == "" {
		password = randomHex(10)
		generated = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, user := range s.state.Users {
		normalizeUser(user)
		if user.Role == RoleSuperAdmin && user.Status != StatusDeleted {
			return nil
		}
	}
	for _, user := range s.state.Users {
		if strings.EqualFold(user.Username, username) {
			user.Role = RoleSuperAdmin
			user.Status = StatusActive
			user.UpdatedAt = time.Now()
			if err := s.saveLocked(); err != nil {
				return err
			}
			log.Printf("已将已有账号 %q 提升为超级管理员", username)
			return nil
		}
	}

	now := time.Now()
	salt := randomHex(16)
	user := &User{
		ID:           randomHex(16),
		Username:     username,
		Nickname:     "超级管理员",
		Role:         RoleSuperAdmin,
		Status:       StatusActive,
		PasswordSalt: salt,
		PasswordHash: hashPassword(password, salt),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.state.Users[user.ID] = user
	if err := s.saveLocked(); err != nil {
		return err
	}
	if generated {
		log.Printf("已创建超级管理员账号 username=%s password=%s，请登录后尽快修改密码", username, password)
	} else {
		log.Printf("已创建超级管理员账号 username=%s", username)
	}
	return nil
}

func (s *Store) Register(username, password string) (*PublicUser, string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, "", errors.New("username required")
	}
	if len(password) < 6 {
		return nil, "", errors.New("password must be at least 6 characters")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, u := range s.state.Users {
		if strings.EqualFold(u.Username, username) {
			return nil, "", errors.New("username already exists")
		}
	}

	now := time.Now()
	salt := randomHex(16)
	user := &User{
		ID:           randomHex(16),
		Username:     username,
		Nickname:     username,
		Role:         RoleUser,
		Status:       StatusActive,
		PasswordSalt: salt,
		PasswordHash: hashPassword(password, salt),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	token := randomHex(32)
	s.state.Users[user.ID] = user
	s.state.Sessions[token] = &Session{Token: token, UserID: user.ID, CreatedAt: now}
	if err := s.saveLocked(); err != nil {
		return nil, "", err
	}
	return user.Public(), token, nil
}

func (s *Store) Login(username, password string) (*PublicUser, string, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, "", errors.New("username and password required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var user *User
	for _, u := range s.state.Users {
		if strings.EqualFold(u.Username, username) {
			user = u
			break
		}
	}
	if user == nil || user.PasswordHash != hashPassword(password, user.PasswordSalt) {
		return nil, "", errors.New("invalid username or password")
	}
	normalizeUser(user)
	if user.Status != StatusActive {
		return nil, "", errors.New("user is not active")
	}

	token := randomHex(32)
	s.state.Sessions[token] = &Session{Token: token, UserID: user.ID, CreatedAt: time.Now()}
	if err := s.saveLocked(); err != nil {
		return nil, "", err
	}
	return user.Public(), token, nil
}

func (s *Store) UserByToken(token string) (*PublicUser, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("missing token")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.state.Sessions[token]
	if !ok {
		return nil, errors.New("invalid token")
	}
	user, ok := s.state.Users[session.UserID]
	if !ok {
		return nil, errors.New("user not found")
	}
	normalizeUser(user)
	if user.Status != StatusActive {
		return nil, errors.New("user is not active")
	}
	return user.Public(), nil
}

func (s *Store) UpdateProfile(userID, nickname, avatarURL string) (*PublicUser, error) {
	userID = strings.TrimSpace(userID)
	nickname = strings.TrimSpace(nickname)
	avatarURL = strings.TrimSpace(avatarURL)
	if userID == "" {
		return nil, errors.New("user id required")
	}
	if len([]rune(nickname)) > 40 {
		return nil, errors.New("nickname must be 40 characters or fewer")
	}
	if len(avatarURL) > 1000 {
		return nil, errors.New("avatar url is too long")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.state.Users[userID]
	if !ok {
		return nil, errors.New("user not found")
	}
	normalizeUser(user)
	user.Nickname = nickname
	user.AvatarURL = avatarURL
	user.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return user.Public(), nil
}

func (s *Store) AddHistory(userID string, h GenerationHistory) error {
	if userID == "" {
		return errors.New("user id required")
	}
	if h.ID == "" {
		h.ID = randomHex(12)
	}
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now()
	}
	h.UserID = userID

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Histories[userID] = append([]GenerationHistory{h}, s.state.Histories[userID]...)
	if len(s.state.Histories[userID]) > 200 {
		s.state.Histories[userID] = s.state.Histories[userID][:200]
	}
	return s.saveLocked()
}

func (s *Store) ListHistory(userID string) []GenerationHistory {
	s.mu.RLock()
	defer s.mu.RUnlock()

	history := append([]GenerationHistory(nil), s.state.Histories[userID]...)
	sort.SliceStable(history, func(i, j int) bool {
		return history[i].CreatedAt.After(history[j].CreatedAt)
	})
	return history
}

func (s *Store) CreateAgentWork(userID, sessionID, theme string) (*AgentWork, error) {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	theme = strings.TrimSpace(theme)
	if userID == "" {
		return nil, errors.New("user id required")
	}
	if sessionID == "" {
		return nil, errors.New("session id required")
	}
	if theme == "" {
		theme = "未命名插画任务"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.AgentWorks == nil {
		s.state.AgentWorks = make(map[string]map[string]*AgentWork)
	}
	if s.state.AgentWorks[userID] == nil {
		s.state.AgentWorks[userID] = make(map[string]*AgentWork)
	}
	if _, exists := s.state.AgentWorks[userID][sessionID]; exists {
		return nil, errors.New("agent work session already exists")
	}
	now := time.Now()
	work := &AgentWork{
		ID:           sessionID,
		SessionID:    sessionID,
		UserID:       userID,
		Kind:         "agent_work",
		ResourceType: "illustration_agent",
		ModelName:    "illustration-agent",
		Theme:        theme,
		Status:       "processing",
		Summary:      "插画 Agent 任务已开始",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.state.AgentWorks[userID][sessionID] = work
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return cloneAgentWork(work), nil
}

func (s *Store) UpdateAgentWorkSnapshot(userID, sessionID, status, errorMessage string, snapshot AgentWorkSnapshot) error {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	work := s.state.AgentWorks[userID][sessionID]
	if work == nil {
		return nil
	}
	if strings.TrimSpace(status) != "" {
		work.Status = strings.TrimSpace(status)
	}
	if errorMessage != "" {
		work.ErrorMessage = errorMessage
	}
	applyAgentWorkSnapshot(work, snapshot)
	work.PreviewURL = agentWorkPreviewURL(work)
	work.Summary = agentWorkSummary(work)
	work.UpdatedAt = time.Now()
	return s.saveLocked()
}

func (s *Store) GetAgentWork(userID, sessionID string) (*AgentWork, error) {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	if userID == "" || sessionID == "" {
		return nil, errors.New("user id and session id required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	work := s.state.AgentWorks[userID][sessionID]
	if work == nil {
		return nil, errors.New("agent work not found")
	}
	return cloneAgentWork(work), nil
}

func (s *Store) ListAgentWorks(userID string) []AgentWork {
	s.mu.RLock()
	defer s.mu.RUnlock()

	worksBySession := s.state.AgentWorks[userID]
	works := make([]AgentWork, 0, len(worksBySession))
	for _, work := range worksBySession {
		if work != nil {
			works = append(works, *cloneAgentWork(work))
		}
	}
	sort.SliceStable(works, func(i, j int) bool {
		return works[i].UpdatedAt.After(works[j].UpdatedAt)
	})
	return works
}

func (s *Store) ListMergedHistory(userID string) []HistoryItem {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]HistoryItem, 0, len(s.state.Histories[userID])+len(s.state.AgentWorks[userID]))
	for _, h := range s.state.Histories[userID] {
		if (h.Kind == "agent_stream" || h.Kind == "agent_resume") && s.state.AgentWorks[userID][h.TaskID] != nil {
			continue
		}
		items = append(items, HistoryItem{
			ItemType:     "history",
			ID:           h.ID,
			UserID:       h.UserID,
			Kind:         h.Kind,
			ResourceType: h.ResourceType,
			ModelName:    h.ModelName,
			Prompt:       h.Prompt,
			Status:       h.Status,
			TaskID:       h.TaskID,
			PreviewURL:   h.PreviewURL,
			Summary:      h.Summary,
			CreatedAt:    h.CreatedAt,
			UpdatedAt:    h.CreatedAt,
		})
	}
	for _, work := range s.state.AgentWorks[userID] {
		if work == nil {
			continue
		}
		items = append(items, HistoryItem{
			ItemType:     "agent_work",
			ID:           work.ID,
			SessionID:    work.SessionID,
			UserID:       work.UserID,
			Kind:         work.Kind,
			ResourceType: work.ResourceType,
			ModelName:    work.ModelName,
			Prompt:       work.Theme,
			Theme:        work.Theme,
			Status:       work.Status,
			TaskID:       work.SessionID,
			PreviewURL:   work.PreviewURL,
			Summary:      work.Summary,
			CreatedAt:    work.CreatedAt,
			UpdatedAt:    work.UpdatedAt,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}

func (s *Store) UserOwnsTask(userID, taskID string) bool {
	if userID == "" || taskID == "" {
		return false
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, h := range s.state.Histories[userID] {
		if h.TaskID == taskID {
			return true
		}
	}
	return false
}

func (s *Store) AddUsage(userID, modelName string, promptTokens, completionTokens int) error {
	if userID == "" {
		return errors.New("user id required")
	}
	modelName = strings.TrimSpace(modelName)
	if modelName == "" {
		modelName = "unknown"
	}
	if promptTokens < 0 {
		promptTokens = 0
	}
	if completionTokens < 0 {
		completionTokens = 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state.Usage[userID] == nil {
		s.state.Usage[userID] = make(map[string]*UsageStat)
	}
	stat := s.state.Usage[userID][modelName]
	if stat == nil {
		stat = &UsageStat{ModelName: modelName}
		s.state.Usage[userID][modelName] = stat
	}
	stat.RequestCount++
	stat.PromptTokens += promptTokens
	stat.CompletionTokens += completionTokens
	stat.TotalTokens = stat.PromptTokens + stat.CompletionTokens
	stat.UpdatedAt = time.Now()
	return s.saveLocked()
}

func (s *Store) ListUsage(userID string) []UsageStat {
	s.mu.RLock()
	defer s.mu.RUnlock()

	statsByModel := s.state.Usage[userID]
	stats := make([]UsageStat, 0, len(statsByModel))
	for _, stat := range statsByModel {
		stats = append(stats, *stat)
	}
	sort.SliceStable(stats, func(i, j int) bool {
		return stats[i].UpdatedAt.After(stats[j].UpdatedAt)
	})
	return stats
}

func applyAgentWorkSnapshot(work *AgentWork, snapshot AgentWorkSnapshot) {
	if strings.TrimSpace(snapshot.State) != "" {
		work.State = strings.TrimSpace(snapshot.State)
	}
	if snapshot.Story != nil {
		story := *snapshot.Story
		story.Chapters = append([]model.StoryChapter(nil), snapshot.Story.Chapters...)
		work.Story = &story
		if strings.TrimSpace(work.Theme) == "" {
			work.Theme = strings.TrimSpace(story.Theme)
		}
	}
	if snapshot.Characters != nil {
		work.Characters = cloneCharacters(snapshot.Characters)
	}
	if snapshot.ImagePrompts != nil {
		work.ImagePrompts = append([]model.ImagePrompt(nil), snapshot.ImagePrompts...)
	}
	if snapshot.GeneratedImages != nil {
		work.GeneratedImages = cloneStringSliceMap(snapshot.GeneratedImages)
	}
	if snapshot.ConfirmedImages != nil {
		work.ConfirmedImages = cloneStringSliceMap(snapshot.ConfirmedImages)
	}
	if snapshot.CurrentImageChapter >= 0 {
		work.CurrentImageChapter = snapshot.CurrentImageChapter
	}
	if snapshot.VideoPrompt != "" {
		work.VideoPrompt = snapshot.VideoPrompt
	}
	if snapshot.ChapterVideoPrompts != nil {
		work.ChapterVideoPrompts = append([]model.VideoPrompt(nil), snapshot.ChapterVideoPrompts...)
	}
	if snapshot.ChapterVideoURLs != nil {
		work.ChapterVideoURLs = cloneStringMap(snapshot.ChapterVideoURLs)
	}
	if snapshot.ChapterAudioURLs != nil {
		work.ChapterAudioURLs = cloneStringMap(snapshot.ChapterAudioURLs)
	}
	if snapshot.NarratedChapterVideoURLs != nil {
		work.NarratedChapterVideoURLs = cloneStringMap(snapshot.NarratedChapterVideoURLs)
	}
	if snapshot.VideoURL != "" {
		work.VideoURL = snapshot.VideoURL
	}
	if snapshot.SelectedVoiceID != "" {
		work.SelectedVoiceID = snapshot.SelectedVoiceID
	}
	if snapshot.SelectedVoiceType != "" {
		work.SelectedVoiceType = snapshot.SelectedVoiceType
	}
	if snapshot.SelectedVoiceName != "" {
		work.SelectedVoiceName = snapshot.SelectedVoiceName
	}
}

func agentWorkPreviewURL(work *AgentWork) string {
	if work == nil {
		return ""
	}
	if work.VideoURL != "" {
		return work.VideoURL
	}
	for _, url := range work.NarratedChapterVideoURLs {
		if url != "" {
			return url
		}
	}
	for _, urls := range work.ConfirmedImages {
		if len(urls) > 0 && urls[0] != "" {
			return urls[0]
		}
	}
	for _, character := range work.Characters {
		if len(character.ReferenceImageURLs) > 0 && character.ReferenceImageURLs[0] != "" {
			return character.ReferenceImageURLs[0]
		}
	}
	return ""
}

func agentWorkSummary(work *AgentWork) string {
	if work == nil {
		return ""
	}
	if work.ErrorMessage != "" {
		return work.ErrorMessage
	}
	if work.VideoURL != "" {
		return "完整插画视频已生成"
	}
	if work.Story != nil && len(work.Story.Chapters) > 0 {
		return fmt.Sprintf("已生成 %d 个故事章节", len(work.Story.Chapters))
	}
	return "插画 Agent 任务进行中"
}

func cloneAgentWork(work *AgentWork) *AgentWork {
	if work == nil {
		return nil
	}
	clone := *work
	if work.Story != nil {
		story := *work.Story
		story.Chapters = append([]model.StoryChapter(nil), work.Story.Chapters...)
		clone.Story = &story
	}
	clone.Characters = cloneCharacters(work.Characters)
	clone.ImagePrompts = append([]model.ImagePrompt(nil), work.ImagePrompts...)
	clone.GeneratedImages = cloneStringSliceMap(work.GeneratedImages)
	clone.ConfirmedImages = cloneStringSliceMap(work.ConfirmedImages)
	clone.ChapterVideoPrompts = append([]model.VideoPrompt(nil), work.ChapterVideoPrompts...)
	clone.ChapterVideoURLs = cloneStringMap(work.ChapterVideoURLs)
	clone.ChapterAudioURLs = cloneStringMap(work.ChapterAudioURLs)
	clone.NarratedChapterVideoURLs = cloneStringMap(work.NarratedChapterVideoURLs)
	return &clone
}

func cloneCharacters(characters []model.CharacterProfile) []model.CharacterProfile {
	if characters == nil {
		return nil
	}
	clones := make([]model.CharacterProfile, len(characters))
	for i, character := range characters {
		clones[i] = character
		clones[i].Aliases = append([]string(nil), character.Aliases...)
		clones[i].ChapterIndices = append([]int(nil), character.ChapterIndices...)
		clones[i].ReferenceImageURLs = append([]string(nil), character.ReferenceImageURLs...)
	}
	return clones
}

func cloneStringSliceMap(src map[int][]string) map[int][]string {
	if src == nil {
		return nil
	}
	dst := make(map[int][]string, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}

func cloneStringMap(src map[int]string) map[int]string {
	if src == nil {
		return nil
	}
	dst := make(map[int]string, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func (u *User) Public() *PublicUser {
	return &PublicUser{
		ID:        u.ID,
		Username:  u.Username,
		Nickname:  u.Nickname,
		AvatarURL: u.AvatarURL,
		Role:      u.Role,
		Status:    u.Status,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

func EstimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	runes := len([]rune(text))
	tokens := (runes + 3) / 4
	if tokens < 1 {
		return 1
	}
	return tokens
}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, &s.state); err != nil {
		return err
	}
	s.ensureState()
	return nil
}

func (s *Store) ensureState() {
	if s.state.Users == nil {
		s.state.Users = make(map[string]*User)
	}
	if s.state.Sessions == nil {
		s.state.Sessions = make(map[string]*Session)
	}
	if s.state.Histories == nil {
		s.state.Histories = make(map[string][]GenerationHistory)
	}
	if s.state.Usage == nil {
		s.state.Usage = make(map[string]map[string]*UsageStat)
	}
	if s.state.Personas == nil {
		s.state.Personas = make(map[string][]Persona)
	}
	if s.state.Voices == nil {
		s.state.Voices = make(map[string][]VoiceProfile)
	}
	if s.state.AgentWorks == nil {
		s.state.AgentWorks = make(map[string]map[string]*AgentWork)
	}
	for _, user := range s.state.Users {
		normalizeUser(user)
	}
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0600)
}

func hashPassword(password, salt string) string {
	sum := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(sum[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		sum := sha256.Sum256([]byte(time.Now().String()))
		return hex.EncodeToString(sum[:n])
	}
	return hex.EncodeToString(b)
}

func normalizeUser(user *User) {
	if user == nil {
		return
	}
	if user.Role == "" {
		user.Role = RoleUser
	}
	if user.Status == "" {
		user.Status = StatusActive
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = user.CreatedAt
	}
}

func validateRole(role string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return RoleUser, nil
	}
	if role != RoleUser && role != RoleSuperAdmin {
		return "", fmt.Errorf("unsupported role: %s", role)
	}
	return role, nil
}

func validateStatus(status string) (string, error) {
	status = strings.TrimSpace(status)
	if status == "" {
		return StatusActive, nil
	}
	switch status {
	case StatusActive, StatusDisabled, StatusDeleted:
		return status, nil
	default:
		return "", fmt.Errorf("unsupported status: %s", status)
	}
}
