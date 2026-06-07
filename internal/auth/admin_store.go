package auth

import (
	"errors"
	"sort"
	"strings"
	"time"
)

type AdminUserInput struct {
	Username  string
	Password  string
	Nickname  string
	AvatarURL string
	Role      string
	Status    string
}

type AdminUserDetail struct {
	User     *PublicUser         `json:"user"`
	History  []GenerationHistory `json:"history"`
	Usage    []UsageStat         `json:"usage"`
	Personas []Persona           `json:"personas"`
	Voices   []VoiceProfile      `json:"voices"`
}

func (s *Store) ListUsers(keyword, role, status string) []*PublicUser {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	role = strings.TrimSpace(role)
	status = strings.TrimSpace(status)

	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*PublicUser, 0, len(s.state.Users))
	for _, user := range s.state.Users {
		normalizeUser(user)
		if role != "" && user.Role != role {
			continue
		}
		if status != "" && user.Status != status {
			continue
		}
		if keyword != "" {
			haystack := strings.ToLower(user.Username + " " + user.Nickname + " " + user.ID)
			if !strings.Contains(haystack, keyword) {
				continue
			}
		}
		users = append(users, user.Public())
	}
	sort.SliceStable(users, func(i, j int) bool {
		return users[i].CreatedAt.After(users[j].CreatedAt)
	})
	return users
}

func (s *Store) AdminCreateUser(input AdminUserInput) (*PublicUser, error) {
	username := strings.TrimSpace(input.Username)
	password := input.Password
	if username == "" {
		return nil, errors.New("username required")
	}
	if len(password) < 6 {
		return nil, errors.New("password must be at least 6 characters")
	}
	role, err := validateRole(input.Role)
	if err != nil {
		return nil, err
	}
	status, err := validateStatus(input.Status)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, user := range s.state.Users {
		if strings.EqualFold(user.Username, username) {
			return nil, errors.New("username already exists")
		}
	}
	now := time.Now()
	salt := randomHex(16)
	nickname := strings.TrimSpace(input.Nickname)
	if nickname == "" {
		nickname = username
	}
	user := &User{
		ID:           randomHex(16),
		Username:     username,
		Nickname:     nickname,
		AvatarURL:    strings.TrimSpace(input.AvatarURL),
		Role:         role,
		Status:       status,
		PasswordSalt: salt,
		PasswordHash: hashPassword(password, salt),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.state.Users[user.ID] = user
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return user.Public(), nil
}

func (s *Store) AdminGetUser(userID string) (*AdminUserDetail, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("user id required")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.state.Users[userID]
	if !ok {
		return nil, errors.New("user not found")
	}
	normalizeUser(user)
	return &AdminUserDetail{
		User:     user.Public(),
		History:  cloneHistory(s.state.Histories[userID]),
		Usage:    cloneUsage(s.state.Usage[userID]),
		Personas: clonePersonas(s.state.Personas[userID]),
		Voices:   cloneVoices(s.state.Voices[userID]),
	}, nil
}

func (s *Store) AdminUpdateUser(userID string, input AdminUserInput) (*PublicUser, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errors.New("user id required")
	}
	role, err := validateRole(input.Role)
	if err != nil {
		return nil, err
	}
	status, err := validateStatus(input.Status)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.state.Users[userID]
	if !ok {
		return nil, errors.New("user not found")
	}
	normalizeUser(user)
	user.Nickname = strings.TrimSpace(input.Nickname)
	user.AvatarURL = strings.TrimSpace(input.AvatarURL)
	user.Role = role
	if status != user.Status && status == StatusDeleted {
		user.DeletedAt = time.Now()
	} else if status != StatusDeleted {
		user.DeletedAt = time.Time{}
	}
	user.Status = status
	user.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return user.Public(), nil
}

func (s *Store) AdminResetPassword(userID, password string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return errors.New("user id required")
	}
	if len(password) < 6 {
		return errors.New("password must be at least 6 characters")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.state.Users[userID]
	if !ok {
		return errors.New("user not found")
	}
	salt := randomHex(16)
	user.PasswordSalt = salt
	user.PasswordHash = hashPassword(password, salt)
	user.UpdatedAt = time.Now()
	if err := s.saveLocked(); err != nil {
		return err
	}
	return nil
}

func (s *Store) AdminSoftDeleteUser(userID string) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return errors.New("user id required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.state.Users[userID]
	if !ok {
		return errors.New("user not found")
	}
	normalizeUser(user)
	user.Status = StatusDeleted
	user.DeletedAt = time.Now()
	user.UpdatedAt = user.DeletedAt
	for token, session := range s.state.Sessions {
		if session.UserID == userID {
			delete(s.state.Sessions, token)
		}
	}
	return s.saveLocked()
}

func cloneHistory(history []GenerationHistory) []GenerationHistory {
	items := append([]GenerationHistory(nil), history...)
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items
}

func cloneUsage(statsByModel map[string]*UsageStat) []UsageStat {
	stats := make([]UsageStat, 0, len(statsByModel))
	for _, stat := range statsByModel {
		stats = append(stats, *stat)
	}
	sort.SliceStable(stats, func(i, j int) bool {
		return stats[i].UpdatedAt.After(stats[j].UpdatedAt)
	})
	return stats
}

func clonePersonas(personas []Persona) []Persona {
	items := append([]Persona(nil), personas...)
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}

func cloneVoices(voices []VoiceProfile) []VoiceProfile {
	items := append([]VoiceProfile(nil), voices...)
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}
