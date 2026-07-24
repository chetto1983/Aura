package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/semindex"
)

const (
	proofProfile = "balanced"
	proofK       = 5
)

type trainingSurface struct {
	Scenarios []scenario `json:"scenarios"`
	Outcomes  []outcome  `json:"outcomes"`
}

type trainingPoint struct {
	contextID string
	domain    string
	action    string
	vec       []float64
	reward    float64
}

type choiceEvaluation struct {
	contextID string
	family    string
	domain    string
	action    string
	reward    float64
	best      float64
	accuracy  float64
}

type actionPrediction struct {
	action string
	score  float64
}

func evaluateProof(
	ctx context.Context,
	holdout []scenario,
	live []outcome,
	embedder semindex.Embedder,
	repeats int,
) (proofReport, error) {
	trainingScenarios, points, err := loadTrainingPoints()
	if err != nil {
		return proofReport{}, err
	}
	if err := validateCompleteSurface(holdout, live, repeats); err != nil {
		return proofReport{}, err
	}

	classifier := prompt.NewReasoningClassifier(embedder, nil)
	staticChoices := make(map[string]string, len(holdout))
	learnedChoices := make(map[string]string, len(holdout))
	predictions := make(map[string]map[string]float64, len(holdout))
	for _, sc := range holdout {
		static, err := operationalStaticChoice(ctx, classifier, sc)
		if err != nil {
			return proofReport{}, err
		}
		staticChoices[sc.ID] = static
		pred := predictActions(points, sc, proofK)
		if len(pred) != len(actions(sc.Domain)) {
			return proofReport{}, fmt.Errorf("%s: predicted %d actions, want %d", sc.ID, len(pred), len(actions(sc.Domain)))
		}
		predictions[sc.ID] = make(map[string]float64, len(pred))
		for _, p := range pred {
			predictions[sc.ID][p.action] = p.score
		}
		learnedChoices[sc.ID] = pred[0].action
	}

	staticEval, err := evaluateChoices(holdout, live, staticChoices)
	if err != nil {
		return proofReport{}, err
	}
	learnedEval, err := evaluateChoices(holdout, live, learnedChoices)
	if err != nil {
		return proofReport{}, err
	}
	staticMetric := summarizePolicy("operational_static", staticEval, nil)
	learnedMetric := summarizePolicy("heldout_graph_knn", learnedEval, staticEval)
	ciLo, ciHi := bootstrapFamilyDelta(learnedEval, staticEval, 10_000, 10201)
	learnedMetric.DeltaCILower = ciLo
	learnedMetric.DeltaCIUpper = ciHi
	learnedMetric.NonInferior = ciLo >= -0.01
	learnedMetric.StrictlyBetter = ciLo > 0

	ope, deterministicRejected, err := runOPE(holdout, live, staticChoices, learnedChoices, predictions)
	if err != nil {
		return proofReport{}, err
	}
	opePassed := deterministicRejected
	for _, metric := range ope {
		if metric.Estimator == "snips" || metric.Estimator == "doubly_robust" {
			opePassed = opePassed &&
				metric.Supported &&
				metric.EffectiveN >= 1_000 &&
				metric.AbsoluteError <= 0.03 &&
				metric.CoversTruth
		}
	}
	generalizationPassed := learnedMetric.StrictlyBetter &&
		learnedMetric.Accuracy >= staticMetric.Accuracy &&
		learnedMetric.HarmfulRate <= staticMetric.HarmfulRate
	verdict := "INVALIDATED"
	if generalizationPassed && opePassed {
		verdict = "VALIDATED"
	} else if learnedMetric.NonInferior && opePassed {
		verdict = "PARTIAL"
	}

	families := map[string]struct{}{}
	for _, sc := range holdout {
		families[familyOf(sc)] = struct{}{}
	}
	return proofReport{
		TrainingContexts:         trainingScenarios,
		HoldoutContexts:          len(holdout),
		HoldoutFamilies:          len(families),
		LiveRunsPerAction:        repeats,
		CatalogChanged:           true,
		Model:                    "unsloth/Qwen3.5-2B-GGUF:Qwen3.5-2B-Q4_K_M.gguf",
		Profile:                  proofProfile,
		K:                        proofK,
		PolicyMetrics:            []policyMetric{staticMetric, learnedMetric},
		OPE:                      ope,
		DeterministicLogRejected: deterministicRejected,
		GeneralizationPassed:     generalizationPassed,
		OPEPassed:                opePassed,
		Verdict:                  verdict,
		Limitations: []string{
			"Evidence is scoped to the exact Qwen3.5 2B GGUF, Granite embedding model, tested catalogs, and authored holdout families.",
			"Two live repetitions per action estimate local model variance but do not establish universal provider or population behavior.",
			"OPE validation uses a safe simulated randomized logger over live measured outcomes; production promotion still requires real supported canary traffic.",
		},
	}, nil
}

func loadTrainingPoints() (int, []trainingPoint, error) {
	paths := []string{
		".planning/spikes/097-qwen-multidomain-reward-surface/artifacts/reward-surface-run1.json",
		".planning/spikes/097-qwen-multidomain-reward-surface/artifacts/reward-surface-run2.json",
	}
	var points []trainingPoint
	contexts := map[string]struct{}{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return 0, nil, fmt.Errorf("read training surface %s: %w", path, err)
		}
		var surface trainingSurface
		if err := json.Unmarshal(raw, &surface); err != nil {
			return 0, nil, fmt.Errorf("decode training surface %s: %w", path, err)
		}
		byID := make(map[string]scenario, len(surface.Scenarios))
		for _, sc := range surface.Scenarios {
			byID[sc.ID] = sc
			contexts[sc.ID] = struct{}{}
		}
		for _, result := range surface.Outcomes {
			sc, ok := byID[result.ScenarioID]
			if !ok || len(sc.Embedding) == 0 {
				return 0, nil, fmt.Errorf("%s: missing scenario/embedding for %s", path, result.ScenarioID)
			}
			reward, ok := result.Rewards[proofProfile]
			if !ok {
				return 0, nil, fmt.Errorf("%s: %s/%s missing %s reward", path, result.ScenarioID, result.Action, proofProfile)
			}
			points = append(points, trainingPoint{
				contextID: result.ScenarioID,
				domain:    result.Domain,
				action:    result.Action,
				vec:       semindex.Normalize(sc.Embedding),
				reward:    reward,
			})
		}
	}
	return len(contexts), points, nil
}

func operationalStaticChoice(
	ctx context.Context,
	classifier *prompt.ReasoningClassifier,
	sc scenario,
) (string, error) {
	switch sc.Domain {
	case "reasoning":
		tier, ok := classifier.Classify(ctx, sc.Prompt)
		if !ok {
			return "", fmt.Errorf("%s: production reasoning classifier failed", sc.ID)
		}
		switch tier {
		case prompt.ReasoningTierNone:
			return "off", nil
		case prompt.ReasoningTierLow:
			return "low_256", nil
		case prompt.ReasoningTierHigh:
			return "high_768", nil
		default:
			return "", fmt.Errorf("%s: unknown reasoning tier %q", sc.ID, tier)
		}
	case "tool":
		// Aura's current deferred-tool path performs semantic ranking and lets the
		// model choose from a shortlist, which qwen_top3 is the closest measured action.
		return "qwen_top3", nil
	case "skill":
		// Aura exposes one multiplexed skill tool containing the full eligible manifest.
		return "qwen_full", nil
	case "knowledge":
		// Current document search is vector retrieval; graph expansion is the challenger.
		return "vector_top1", nil
	default:
		return "", fmt.Errorf("%s: unknown domain %q", sc.ID, sc.Domain)
	}
}

func predictActions(points []trainingPoint, sc scenario, k int) []actionPrediction {
	query := semindex.Normalize(sc.Embedding)
	result := make([]actionPrediction, 0, len(actions(sc.Domain)))
	for _, action := range actions(sc.Domain) {
		candidates := make([]struct {
			sim    float64
			reward float64
		}, 0)
		for _, p := range points {
			if p.domain != sc.Domain || p.action != action {
				continue
			}
			candidates = append(candidates, struct {
				sim    float64
				reward float64
			}{sim: cosine(query, p.vec), reward: p.reward})
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].sim > candidates[j].sim })
		if len(candidates) > k {
			candidates = candidates[:k]
		}
		sum, weights := 0.0, 0.0
		for _, item := range candidates {
			// The governed policy's fixed confidence-free holdout form. Live
			// deterministic quality evidence has confidence 1; similarity^3 makes
			// semantically distant training examples contribute conservatively.
			w := math.Pow(math.Max(item.sim, 0), 3)
			sum += w * item.reward
			weights += w
		}
		score := -0.25
		if weights > 0 {
			score = sum / weights
		}
		result = append(result, actionPrediction{action: action, score: score})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].score == result[j].score {
			return result[i].action < result[j].action
		}
		return result[i].score > result[j].score
	})
	return result
}

func evaluateChoices(holdout []scenario, live []outcome, choices map[string]string) ([]choiceEvaluation, error) {
	byCell := make(map[string][]outcome)
	for _, result := range live {
		byCell[result.ScenarioID+"\x00"+result.Action] = append(byCell[result.ScenarioID+"\x00"+result.Action], result)
	}
	eval := make([]choiceEvaluation, 0, len(holdout))
	for _, sc := range holdout {
		action := choices[sc.ID]
		chosen := byCell[sc.ID+"\x00"+action]
		if len(chosen) == 0 {
			return nil, fmt.Errorf("%s: no holdout outcome for chosen action %s", sc.ID, action)
		}
		reward, accuracy := meanCell(chosen)
		best := -math.MaxFloat64
		for _, candidate := range actions(sc.Domain) {
			rows := byCell[sc.ID+"\x00"+candidate]
			if len(rows) == 0 {
				return nil, fmt.Errorf("%s: no holdout outcomes for candidate %s", sc.ID, candidate)
			}
			value, _ := meanCell(rows)
			best = math.Max(best, value)
		}
		eval = append(eval, choiceEvaluation{
			contextID: sc.ID,
			family:    familyOf(sc),
			domain:    sc.Domain,
			action:    action,
			reward:    reward,
			best:      best,
			accuracy:  accuracy,
		})
	}
	return eval, nil
}

func meanCell(rows []outcome) (float64, float64) {
	reward, correct := 0.0, 0.0
	for _, row := range rows {
		reward += row.Rewards[proofProfile]
		if row.Correct {
			correct++
		}
	}
	n := float64(len(rows))
	return reward / n, correct / n
}

func summarizePolicy(name string, rows, baseline []choiceEvaluation) policyMetric {
	var utility, regret, accuracy float64
	families := map[string]struct{}{}
	for _, row := range rows {
		utility += row.reward
		regret += row.best - row.reward
		accuracy += row.accuracy
		families[row.family] = struct{}{}
	}
	n := float64(len(rows))
	metric := policyMetric{
		Policy:          name,
		MeanUtility:     utility / n,
		MeanRegret:      regret / n,
		Accuracy:        accuracy / n,
		HarmfulRate:     1 - accuracy/n,
		ContextFamilies: len(families),
	}
	if len(baseline) == len(rows) && len(rows) > 0 {
		for i := range rows {
			metric.UtilityDelta += rows[i].reward - baseline[i].reward
		}
		metric.UtilityDelta /= n
	}
	return metric
}

func bootstrapFamilyDelta(candidate, baseline []choiceEvaluation, iterations int, seed int64) (float64, float64) {
	byFamily := map[string][]float64{}
	for i := range candidate {
		byFamily[candidate[i].family] = append(byFamily[candidate[i].family], candidate[i].reward-baseline[i].reward)
	}
	families := make([]string, 0, len(byFamily))
	familyMean := map[string]float64{}
	for family, values := range byFamily {
		families = append(families, family)
		for _, value := range values {
			familyMean[family] += value
		}
		familyMean[family] /= float64(len(values))
	}
	sort.Strings(families)
	rng := rand.New(rand.NewSource(seed))
	samples := make([]float64, iterations)
	for i := range samples {
		for range families {
			samples[i] += familyMean[families[rng.Intn(len(families))]]
		}
		samples[i] /= float64(len(families))
	}
	sort.Float64s(samples)
	return quantile(samples, 0.025), quantile(samples, 0.975)
}

func familyOf(sc scenario) string {
	if family := sc.Meta["family"]; family != "" {
		return sc.Domain + "/" + family
	}
	return sc.Domain + "/" + sc.ID
}

func validateCompleteSurface(scenarios []scenario, results []outcome, repeats int) error {
	counts := map[string]int{}
	for _, result := range results {
		if result.Error != "" {
			return fmt.Errorf("%s/%s repeat %d failed: %s", result.ScenarioID, result.Action, result.Repeat, result.Error)
		}
		counts[result.ScenarioID+"\x00"+result.Action]++
	}
	for _, sc := range scenarios {
		for _, action := range actions(sc.Domain) {
			if got := counts[sc.ID+"\x00"+action]; got != repeats {
				return fmt.Errorf("%s/%s live runs=%d, want %d", sc.ID, action, got, repeats)
			}
		}
	}
	return nil
}

func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot
}

func quantile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}
