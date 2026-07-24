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
	Proof         proofReport        `json:"proof"`
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

type policyMetric struct {
	Policy          string  `json:"policy"`
	MeanUtility     float64 `json:"mean_utility"`
	MeanRegret      float64 `json:"mean_regret"`
	Accuracy        float64 `json:"accuracy"`
	HarmfulRate     float64 `json:"harmful_rate"`
	ContextFamilies int     `json:"context_families"`
	UtilityDelta    float64 `json:"utility_delta_vs_static,omitempty"`
	DeltaCILower    float64 `json:"delta_ci_lower,omitempty"`
	DeltaCIUpper    float64 `json:"delta_ci_upper,omitempty"`
	NonInferior     bool    `json:"non_inferior,omitempty"`
	StrictlyBetter  bool    `json:"strictly_better,omitempty"`
}

type opeMetric struct {
	Estimator     string  `json:"estimator"`
	Estimate      float64 `json:"estimate"`
	GroundTruth   float64 `json:"ground_truth"`
	AbsoluteError float64 `json:"absolute_error"`
	EffectiveN    float64 `json:"effective_sample_size"`
	MaxWeight     float64 `json:"max_importance_weight"`
	OverlapMin    float64 `json:"minimum_logging_probability"`
	Supported     bool    `json:"supported"`
	CILower       float64 `json:"ci_lower"`
	CIUpper       float64 `json:"ci_upper"`
	CoversTruth   bool    `json:"covers_ground_truth"`
}

type proofReport struct {
	TrainingContexts         int            `json:"training_contexts"`
	HoldoutContexts          int            `json:"holdout_contexts"`
	HoldoutFamilies          int            `json:"holdout_families"`
	LiveRunsPerAction        int            `json:"live_runs_per_action"`
	CatalogChanged           bool           `json:"catalog_changed"`
	Model                    string         `json:"model"`
	Profile                  string         `json:"profile"`
	K                        int            `json:"k"`
	PolicyMetrics            []policyMetric `json:"policy_metrics"`
	OPE                      []opeMetric    `json:"ope"`
	DeterministicLogRejected bool           `json:"deterministic_log_rejected"`
	GeneralizationPassed     bool           `json:"generalization_passed"`
	OPEPassed                bool           `json:"ope_passed"`
	Verdict                  string         `json:"verdict"`
	Limitations              []string       `json:"limitations"`
}
