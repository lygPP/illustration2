package auth

import (
	"errors"
	"strings"
	"time"
)

type PersonaInput struct {
	Name        string
	Description string
	ImageURL    string
	Status      string
}

type VoiceInput struct {
	Name        string
	Description string
	Status      string
}

type VoiceCloneResult struct {
	ArkTaskID        string
	GeneratedVoiceID string
	VoiceType        string
	PreviewAudioURL  string
	ErrorMessage     string
	Status           string
}

func (s *Store) ListPersonas(userID string) []Persona {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clonePersonas(s.state.Personas[userID])
}

func (s *Store) CreatePersona(userID string, input PersonaInput) (Persona, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Persona{}, errors.New("name required")
	}
	now := time.Now()
	persona := Persona{
		ID:          randomHex(12),
		UserID:      userID,
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		ImageURL:    strings.TrimSpace(input.ImageURL),
		Status:      StatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Personas[userID] = append([]Persona{persona}, s.state.Personas[userID]...)
	if err := s.saveLocked(); err != nil {
		return Persona{}, err
	}
	return persona, nil
}

func (s *Store) UpdatePersona(userID, personaID string, input PersonaInput) (Persona, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return Persona{}, errors.New("name required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.state.Personas[userID]
	for i := range items {
		if items[i].ID == personaID {
			items[i].Name = name
			items[i].Description = strings.TrimSpace(input.Description)
			if strings.TrimSpace(input.ImageURL) != "" {
				items[i].ImageURL = strings.TrimSpace(input.ImageURL)
			}
			items[i].UpdatedAt = time.Now()
			s.state.Personas[userID] = items
			if err := s.saveLocked(); err != nil {
				return Persona{}, err
			}
			return items[i], nil
		}
	}
	return Persona{}, errors.New("persona not found")
}

func (s *Store) UpdatePersonaImage(userID, personaID, imageURL string) (Persona, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return Persona{}, errors.New("image url required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.state.Personas[userID]
	for i := range items {
		if items[i].ID == personaID {
			items[i].ImageURL = imageURL
			items[i].UpdatedAt = time.Now()
			s.state.Personas[userID] = items
			if err := s.saveLocked(); err != nil {
				return Persona{}, err
			}
			return items[i], nil
		}
	}
	return Persona{}, errors.New("persona not found")
}

func (s *Store) DeletePersona(userID, personaID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.state.Personas[userID]
	for i := range items {
		if items[i].ID == personaID {
			s.state.Personas[userID] = append(items[:i], items[i+1:]...)
			return s.saveLocked()
		}
	}
	return errors.New("persona not found")
}

func (s *Store) ListVoices(userID string) []VoiceProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneVoices(s.state.Voices[userID])
}

func (s *Store) GetVoice(userID, voiceID string) (VoiceProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, voice := range s.state.Voices[userID] {
		if voice.ID == voiceID {
			return voice, nil
		}
	}
	return VoiceProfile{}, errors.New("voice not found")
}

func (s *Store) CreateVoice(userID string, input VoiceInput) (VoiceProfile, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return VoiceProfile{}, errors.New("name required")
	}
	now := time.Now()
	voice := VoiceProfile{
		ID:          randomHex(12),
		UserID:      userID,
		Name:        name,
		Description: strings.TrimSpace(input.Description),
		Status:      "draft",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.state.Voices[userID] = append([]VoiceProfile{voice}, s.state.Voices[userID]...)
	if err := s.saveLocked(); err != nil {
		return VoiceProfile{}, err
	}
	return voice, nil
}

func (s *Store) UpdateVoice(userID, voiceID string, input VoiceInput) (VoiceProfile, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return VoiceProfile{}, errors.New("name required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.state.Voices[userID]
	for i := range items {
		if items[i].ID == voiceID {
			items[i].Name = name
			items[i].Description = strings.TrimSpace(input.Description)
			items[i].UpdatedAt = time.Now()
			s.state.Voices[userID] = items
			if err := s.saveLocked(); err != nil {
				return VoiceProfile{}, err
			}
			return items[i], nil
		}
	}
	return VoiceProfile{}, errors.New("voice not found")
}

func (s *Store) UpdateVoiceSample(userID, voiceID, sampleURL string) (VoiceProfile, error) {
	sampleURL = strings.TrimSpace(sampleURL)
	if sampleURL == "" {
		return VoiceProfile{}, errors.New("sample url required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.state.Voices[userID]
	for i := range items {
		if items[i].ID == voiceID {
			items[i].SampleAudioURL = sampleURL
			items[i].Status = "sample_uploaded"
			items[i].ErrorMessage = ""
			items[i].UpdatedAt = time.Now()
			s.state.Voices[userID] = items
			if err := s.saveLocked(); err != nil {
				return VoiceProfile{}, err
			}
			return items[i], nil
		}
	}
	return VoiceProfile{}, errors.New("voice not found")
}

func (s *Store) ApplyVoiceCloneResult(userID, voiceID string, result VoiceCloneResult) (VoiceProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.state.Voices[userID]
	for i := range items {
		if items[i].ID == voiceID {
			if result.ArkTaskID != "" {
				items[i].ArkTaskID = result.ArkTaskID
			}
			if result.GeneratedVoiceID != "" {
				items[i].GeneratedVoiceID = result.GeneratedVoiceID
			}
			if result.VoiceType != "" {
				items[i].VoiceType = result.VoiceType
			}
			if result.PreviewAudioURL != "" {
				items[i].PreviewAudioURL = result.PreviewAudioURL
			}
			items[i].ErrorMessage = result.ErrorMessage
			if result.Status != "" {
				items[i].Status = result.Status
			}
			items[i].UpdatedAt = time.Now()
			s.state.Voices[userID] = items
			if err := s.saveLocked(); err != nil {
				return VoiceProfile{}, err
			}
			return items[i], nil
		}
	}
	return VoiceProfile{}, errors.New("voice not found")
}

func (s *Store) DeleteVoice(userID, voiceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	items := s.state.Voices[userID]
	for i := range items {
		if items[i].ID == voiceID {
			s.state.Voices[userID] = append(items[:i], items[i+1:]...)
			return s.saveLocked()
		}
	}
	return errors.New("voice not found")
}
