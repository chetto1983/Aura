package main

import "time"

type scenario struct {
	ID         string              `json:"id"`
	Domain     string              `json:"domain"`
	Prompt     string              `json:"prompt"`
	Expected   string              `json:"expected"`
	Embedding  []float64           `json:"embedding"`
	Meta       map[string]string   `json:"meta,omitempty"`
	Contexts   map[string]string   `json:"contexts,omitempty"`
	Candidates map[string][]string `json:"candidates,omitempty"`
}

type outcome struct {
	ScenarioID       string             `json:"scenario_id"`
	Domain           string             `json:"domain"`
	Action           string             `json:"action"`
	Repeat           int                `json:"repeat"`
	Correct          bool               `json:"correct"`
	Selected         string             `json:"selected,omitempty"`
	Content          string             `json:"content,omitempty"`
	ReasoningChars   int                `json:"reasoning_chars"`
	PromptTokens     int                `json:"prompt_tokens"`
	CompletionTokens int                `json:"completion_tokens"`
	TotalTokens      int                `json:"total_tokens"`
	LatencyMS        float64            `json:"latency_ms"`
	Rewards          map[string]float64 `json:"rewards,omitempty"`
	Error            string             `json:"error,omitempty"`
}

type rewardSurface struct {
	SchemaVersion string             `json:"schema_version"`
	RunID         string             `json:"run_id"`
	CreatedAt     time.Time          `json:"created_at"`
	Model         string             `json:"model"`
	ModelURL      string             `json:"model_url"`
	EmbedURL      string             `json:"embed_url"`
	EmbedDim      int                `json:"embed_dim"`
	Repeats       int                `json:"repeats"`
	Scenarios     []scenario         `json:"scenarios"`
	Outcomes      []outcome          `json:"outcomes"`
	Summary       map[string]summary `json:"summary"`
}

type summary struct {
	Runs          int     `json:"runs"`
	Accuracy      float64 `json:"accuracy"`
	MeanTokens    float64 `json:"mean_tokens"`
	MeanLatencyMS float64 `json:"mean_latency_ms"`
	ErrorCount    int     `json:"error_count"`
}

type catalogItem struct {
	Name        string
	Description string
}

type knowledgeFact struct {
	ID        string
	Text      string
	RelatedID string
}
