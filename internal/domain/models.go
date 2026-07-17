package domain

import "time"

type ModelProfile struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Provider        string    `json:"provider"`
	APIURL          string    `json:"api_url"`
	APIKeyEncrypted []byte    `json:"-"`
	HasAPIKey       bool      `json:"has_api_key"`
	Model           string    `json:"model"`
	TimeoutSeconds  int       `json:"timeout_seconds"`
	MaxOutputTokens int       `json:"max_output_tokens"`
	ContextWindow   int       `json:"context_window"`
	IsDefault       bool      `json:"is_default"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Character struct {
	ID                    string    `json:"id"`
	Name                  string    `json:"name"`
	AvatarMIME            string    `json:"avatar_mime,omitempty"`
	HasAvatar             bool      `json:"has_avatar"`
	Bio                   string    `json:"bio"`
	Personality           string    `json:"personality"`
	Scenario              string    `json:"scenario"`
	Greeting              string    `json:"greeting"`
	SystemRules           string    `json:"system_rules"`
	ExampleDialogue       string    `json:"example_dialogue"`
	EnableTime            bool      `json:"enable_time"`
	EnableCalculator      bool      `json:"enable_calculator"`
	DefaultModelProfileID *string   `json:"default_model_profile_id"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type CharacterSnapshot struct {
	Name             string `json:"name"`
	Bio              string `json:"bio"`
	Personality      string `json:"personality"`
	Scenario         string `json:"scenario"`
	Greeting         string `json:"greeting"`
	SystemRules      string `json:"system_rules"`
	ExampleDialogue  string `json:"example_dialogue"`
	EnableTime       bool   `json:"enable_time"`
	EnableCalculator bool   `json:"enable_calculator"`
}

type Conversation struct {
	ID                string            `json:"id"`
	Title             string            `json:"title"`
	CharacterID       string            `json:"character_id"`
	CharacterSnapshot CharacterSnapshot `json:"character_snapshot"`
	ModelProfileID    string            `json:"model_profile_id"`
	MemorySummary     string            `json:"memory_summary,omitempty"`
	SummaryThroughSeq int64             `json:"summary_through_seq,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

type Message struct {
	ID              string    `json:"id"`
	ConversationID  string    `json:"conversation_id"`
	Seq             int64     `json:"seq"`
	Role            string    `json:"role"`
	Content         string    `json:"content"`
	Status          string    `json:"status"`
	HiddenToolInfo  string    `json:"-"`
	ClientMessageID string    `json:"client_message_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

const (
	MessageGenerating = "generating"
	MessageCompleted  = "completed"
	MessageFailed     = "failed"
	MessageCancelled  = "cancelled"
)

func (c Character) Snapshot() CharacterSnapshot {
	return CharacterSnapshot{Name: c.Name, Bio: c.Bio, Personality: c.Personality, Scenario: c.Scenario, Greeting: c.Greeting, SystemRules: c.SystemRules, ExampleDialogue: c.ExampleDialogue, EnableTime: c.EnableTime, EnableCalculator: c.EnableCalculator}
}
