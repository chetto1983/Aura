package main

import (
	"math"
	"math/rand"
	"sort"
	"strings"
)

type decision struct {
	Action     string
	Propensity float64
}

type policy interface {
	Name() string
	Choose(sc benchScenario, profile string, features []float64, rng *rand.Rand) decision
	Update(action string, features []float64, reward float64)
}

type staticPolicy struct{}

func (staticPolicy) Name() string                      { return "static_centroid" }
func (staticPolicy) Update(string, []float64, float64) {}
func (staticPolicy) Choose(sc benchScenario, _ string, _ []float64, _ *rand.Rand) decision {
	action := ""
	switch sc.Domain {
	case "reasoning":
		lower := strings.ToLower(sc.Prompt)
		if strings.Contains(lower, "18 multiplied") ||
			strings.Contains(lower, "capital of italy") ||
			strings.Contains(lower, "5 machines") ||
			strings.Contains(lower, "bloops") {
			action = "off"
		} else {
			action = "low_256"
		}
	case "tool", "skill":
		action = "semantic_top1"
	case "knowledge":
		action = "graph_expand"
	}
	return decision{Action: action, Propensity: 1}
}

type observation struct {
	x      []float64
	reward float64
}

type knnPolicy struct {
	history map[string][]observation
	k       int
	epsilon float64
}

func newKNNPolicy() *knnPolicy {
	return &knnPolicy{history: make(map[string][]observation), k: 5, epsilon: 0.04}
}

func (*knnPolicy) Name() string { return "graph_knn" }
func (p *knnPolicy) Choose(sc benchScenario, _ string, features []float64, rng *rand.Rand) decision {
	scores := make(map[string]float64, len(sc.Actions))
	for _, action := range sc.Actions {
		value, ok := p.predict(action, features)
		if !ok {
			value = 0.55
		}
		scores[action] = value
	}
	return epsilonDecision(sc.Actions, scores, p.epsilon, rng)
}
func (p *knnPolicy) Update(action string, features []float64, reward float64) {
	p.history[action] = append(p.history[action], observation{x: append([]float64(nil), features...), reward: reward})
	if len(p.history[action]) > 256 {
		p.history[action] = p.history[action][len(p.history[action])-256:]
	}
}
func (p *knnPolicy) predict(action string, features []float64) (float64, bool) {
	history := p.history[action]
	if len(history) == 0 {
		return 0, false
	}
	type neighbor struct {
		similarity float64
		reward     float64
	}
	neighbors := make([]neighbor, 0, len(history))
	for _, item := range history {
		neighbors = append(neighbors, neighbor{similarity: math.Max(0, dot(features, item.x)), reward: item.reward})
	}
	sort.Slice(neighbors, func(i, j int) bool { return neighbors[i].similarity > neighbors[j].similarity })
	if len(neighbors) > p.k {
		neighbors = neighbors[:p.k]
	}
	var weighted, total float64
	for _, item := range neighbors {
		weight := item.similarity + 1e-6
		weighted += weight * item.reward
		total += weight
	}
	return weighted / total, true
}

type linearArm struct {
	inv [][]float64
	b   []float64
}

type linUCBPolicy struct {
	arms    map[string]*linearArm
	dim     int
	alpha   float64
	epsilon float64
}

func newLinUCBPolicy(dim int) *linUCBPolicy {
	return &linUCBPolicy{arms: make(map[string]*linearArm), dim: dim, alpha: 0.32, epsilon: 0.04}
}

func (*linUCBPolicy) Name() string { return "linucb" }
func (p *linUCBPolicy) Choose(sc benchScenario, _ string, features []float64, rng *rand.Rand) decision {
	scores := make(map[string]float64, len(sc.Actions))
	for _, action := range sc.Actions {
		mean, uncertainty := p.predict(action, features)
		scores[action] = mean + p.alpha*uncertainty
	}
	return epsilonDecision(sc.Actions, scores, p.epsilon, rng)
}
func (p *linUCBPolicy) Update(action string, features []float64, reward float64) {
	arm := p.arm(action)
	updateInverse(arm.inv, features)
	for i := range arm.b {
		arm.b[i] += reward * features[i]
	}
}
func (p *linUCBPolicy) arm(action string) *linearArm {
	if arm := p.arms[action]; arm != nil {
		return arm
	}
	inv := make([][]float64, p.dim)
	for i := range inv {
		inv[i] = make([]float64, p.dim)
		inv[i][i] = 1
	}
	arm := &linearArm{inv: inv, b: make([]float64, p.dim)}
	p.arms[action] = arm
	return arm
}
func (p *linUCBPolicy) predict(action string, features []float64) (float64, float64) {
	arm := p.arm(action)
	theta := matVec(arm.inv, arm.b)
	projected := matVec(arm.inv, features)
	return dot(theta, features), math.Sqrt(math.Max(0, dot(features, projected)))
}

type hybridPolicy struct {
	linear   *linUCBPolicy
	graph    *knnPolicy
	graphMSE map[string]float64
	linMSE   map[string]float64
	count    map[string]int
	epsilon  float64
}

func newHybridPolicy(dim int) *hybridPolicy {
	linear := newLinUCBPolicy(dim)
	linear.epsilon = 0
	graph := newKNNPolicy()
	graph.epsilon = 0
	return &hybridPolicy{
		linear: linear, graph: graph, graphMSE: make(map[string]float64),
		linMSE: make(map[string]float64), count: make(map[string]int), epsilon: 0.04,
	}
}

func (*hybridPolicy) Name() string { return "graph_prior_linucb" }
func (p *hybridPolicy) Choose(sc benchScenario, _ string, features []float64, rng *rand.Rand) decision {
	scores := make(map[string]float64, len(sc.Actions))
	for _, action := range sc.Actions {
		linMean, uncertainty := p.linear.predict(action, features)
		graphMean, ok := p.graph.predict(action, features)
		mean := linMean
		if ok {
			mean = p.weight(action)*graphMean + (1-p.weight(action))*linMean
		}
		scores[action] = mean + p.linear.alpha*uncertainty
	}
	return epsilonDecision(sc.Actions, scores, p.epsilon, rng)
}
func (p *hybridPolicy) Update(action string, features []float64, reward float64) {
	linPrediction, _ := p.linear.predict(action, features)
	graphPrediction, graphOK := p.graph.predict(action, features)
	p.linMSE[action] = ewma(p.linMSE[action], squared(reward-linPrediction), p.count[action])
	if graphOK {
		p.graphMSE[action] = ewma(p.graphMSE[action], squared(reward-graphPrediction), p.count[action])
	}
	p.count[action]++
	p.linear.Update(action, features, reward)
	p.graph.Update(action, features, reward)
}
func (p *hybridPolicy) weight(action string) float64 {
	if p.count[action] < 3 {
		return 0.5
	}
	graphErr := p.graphMSE[action]
	if graphErr == 0 {
		graphErr = 0.25
	}
	linErr := p.linMSE[action]
	if linErr == 0 {
		linErr = 0.25
	}
	weight := linErr / (linErr + graphErr)
	return math.Max(0.1, math.Min(0.9, weight))
}

func epsilonDecision(actions []string, scores map[string]float64, epsilon float64, rng *rand.Rand) decision {
	best := actions[0]
	for _, action := range actions[1:] {
		if scores[action] > scores[best] {
			best = action
		}
	}
	chosen := best
	if rng.Float64() < epsilon {
		chosen = actions[rng.Intn(len(actions))]
	}
	propensity := epsilon / float64(len(actions))
	if chosen == best {
		propensity += 1 - epsilon
	}
	return decision{Action: chosen, Propensity: propensity}
}

func updateInverse(inv [][]float64, x []float64) {
	projected := matVec(inv, x)
	denom := 1 + dot(x, projected)
	for i := range inv {
		for j := range inv[i] {
			inv[i][j] -= projected[i] * projected[j] / denom
		}
	}
}

func matVec(matrix [][]float64, vector []float64) []float64 {
	out := make([]float64, len(matrix))
	for i := range matrix {
		out[i] = dot(matrix[i], vector)
	}
	return out
}

func ewma(previous, sample float64, count int) float64 {
	if count == 0 {
		return sample
	}
	return 0.15*sample + 0.85*previous
}

func squared(value float64) float64 { return value * value }
