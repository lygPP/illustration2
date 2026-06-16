package ill_agent

import (
	"context"
	"errors"
	"fmt"
	"illustration2/internal/auth"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type VoiceSelectionAgent struct {
	AgentName string
	AgentDesc string
}

func NewVoiceSelectionAgent(ctx context.Context) adk.Agent {
	return VoiceSelectionAgent{
		AgentName: "音色选择助手",
		AgentDesc: "在章节视频生成后让用户选择解说音色",
	}
}

func (r VoiceSelectionAgent) Name(ctx context.Context) string {
	return r.AgentName
}

func (r VoiceSelectionAgent) Description(ctx context.Context) string {
	return r.AgentDesc
}

func (r VoiceSelectionAgent) Run(ctx context.Context, input *adk.AgentInput,
	options ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		sessionState := GetSessionState(ctx)
		if ShouldSkipStage(sessionState, StageVoiceSelection) {
			gen.Send(StageSkippedEvent(r.AgentName))
			return
		}

		voices, err := availableVoices(ctx)
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		if len(voices) == 0 {
			gen.Send(&adk.AgentEvent{Err: errors.New("没有可用音色，请先在音色库创建并复刻完成一个音色")})
			return
		}

		sessionState.State = "voice_selection"
		SaveSessionState(ctx, sessionState)

		gen.Send(adk.StatefulInterrupt(ctx, voiceSelectionInfo(voices, ""), sessionState.State))
	}()

	return iter
}

func (r VoiceSelectionAgent) Resume(ctx context.Context, info *adk.ResumeInfo,
	opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer gen.Close()

		if info.ResumeData == nil {
			gen.Send(&adk.AgentEvent{Err: errors.New("voice selection receives nil resume data")})
			return
		}
		choice, ok := info.ResumeData.(string)
		if !ok {
			gen.Send(&adk.AgentEvent{Err: errors.New("voice selection receives invalid resume data")})
			return
		}

		voices, err := availableVoices(ctx)
		if err != nil {
			gen.Send(&adk.AgentEvent{Err: err})
			return
		}
		voice, ok := matchVoiceChoice(voices, choice)
		sessionState := GetSessionState(ctx)
		if !ok {
			sessionState.State = "voice_selection"
			SaveSessionState(ctx, sessionState)
			gen.Send(adk.StatefulInterrupt(ctx, voiceSelectionInfo(voices, "未找到匹配音色，请回复序号、音色ID或音色名称。"), sessionState.State))
			return
		}

		sessionState.SelectedVoiceID = voice.ID
		sessionState.SelectedVoiceType = localVoiceReference(voice)
		sessionState.SelectedVoiceName = voice.Name
		sessionState.State = "voice_selected"
		SaveSessionState(ctx, sessionState)

		gen.Send(&adk.AgentEvent{
			Output: &adk.AgentOutput{
				MessageOutput: &adk.MessageVariant{
					IsStreaming: false,
					Message: &schema.Message{
						Role:    schema.Assistant,
						Content: fmt.Sprintf("已选择音色：%s", voice.Name),
					},
				},
			},
		})
	}()

	return iter
}

func availableVoices(ctx context.Context) ([]auth.VoiceProfile, error) {
	store := GetAgentStore(ctx)
	userID := GetAgentUserID(ctx)
	if store == nil || strings.TrimSpace(userID) == "" {
		return nil, errors.New("agent user context is missing, cannot list voices")
	}
	allVoices := store.ListVoices(userID)
	voices := make([]auth.VoiceProfile, 0, len(allVoices))
	for _, voice := range allVoices {
		if strings.EqualFold(strings.TrimSpace(voice.Status), "ready") && localVoiceReference(voice) != "" {
			voices = append(voices, voice)
		}
	}
	return voices, nil
}

func voiceSelectionInfo(voices []auth.VoiceProfile, message string) []map[string]interface{} {
	info := make([]map[string]interface{}, 0, len(voices)+2)
	if message != "" {
		info = append(info, map[string]interface{}{"text": message})
	}
	info = append(info, map[string]interface{}{"text": "章节视频已生成，请选择用于章节解说语音合成的音色。可回复序号、音色ID或音色名称。"})
	for i, voice := range voices {
		info = append(info, map[string]interface{}{
			"text":            fmt.Sprintf("%d. %s", i+1, voice.Name),
			"voiceIndex":      i + 1,
			"voiceId":         voice.ID,
			"voiceName":       voice.Name,
			"description":     voice.Description,
			"previewAudioUrl": voice.PreviewAudioURL,
		})
	}
	return info
}

func matchVoiceChoice(voices []auth.VoiceProfile, choice string) (auth.VoiceProfile, bool) {
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return auth.VoiceProfile{}, false
	}
	if idx, err := strconv.Atoi(choice); err == nil && idx >= 1 && idx <= len(voices) {
		return voices[idx-1], true
	}
	for _, voice := range voices {
		if strings.EqualFold(choice, voice.ID) || strings.EqualFold(choice, voice.Name) {
			return voice, true
		}
	}
	return auth.VoiceProfile{}, false
}

func localVoiceReference(voice auth.VoiceProfile) string {
	for _, value := range []string{voice.VoiceType, voice.SampleAudioURL} {
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "uploads/") || strings.HasPrefix(value, "resource/") {
			return value
		}
	}
	return ""
}
