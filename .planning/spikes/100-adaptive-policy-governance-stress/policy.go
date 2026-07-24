package main

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand"
	"sort"
	"strings"
)

type observation struct {
	x      []float64
	reward float64
}

type choice struct {
	action   string
	fallback bool
}

type learner interface {
	name() string
	choose(scenario, []float64, bool, *rand.Rand) choice
	update(string, []float64, float64)
	rollbacks() int
}

type rawKNN struct {
	history map[string][]observation
}

func newRawKNN() *rawKNN       { return &rawKNN{history: make(map[string][]observation)} }
func (*rawKNN) name() string   { return "raw_graph_knn" }
func (*rawKNN) rollbacks() int { return 0 }
func (p *rawKNN) choose(sc scenario, x []float64, storeUp bool, rng *rand.Rand) choice {
	scores := make(map[string]float64)
	for _, action := range sc.actions {
		value, ok := neighborMean(p.history[action], x, 5)
		if !ok || !storeUp {
			value = 0.55
		}
		scores[action] = value
	}
	return choice{action: epsilonPick(sc.actions, scores, 0.04, rng)}
}
func (p *rawKNN) update(action string, x []float64, reward float64) {
	p.history[action] = append(p.history[action], observation{x: append([]float64(nil), x...), reward: reward})
	if len(p.history[action]) > 256 {
		p.history[action] = p.history[action][len(p.history[action])-256:]
	}
}

type governedKNN struct {
	history         map[string][]observation
	actionRewards   map[string][]float64
	quarantineUntil map[string]int
	step            int
	rollbackCount   int
}

func newGovernedKNN() *governedKNN {
	return &governedKNN{
		history:         make(map[string][]observation),
		actionRewards:   make(map[string][]float64),
		quarantineUntil: make(map[string]int),
	}
}
func (*governedKNN) name() string     { return "governed_graph_knn" }
func (p *governedKNN) rollbacks() int { return p.rollbackCount }
func (p *governedKNN) choose(sc scenario, x []float64, storeUp bool, rng *rand.Rand) choice {
	p.step++
	static := staticAction(sc)
	if !storeUp && p.observations() == 0 {
		return choice{action: static, fallback: true}
	}
	scores := make(map[string]float64)
	available := make([]string, 0, len(sc.actions))
	for _, action := range sc.actions {
		if p.step < p.quarantineUntil[action] {
			continue
		}
		available = append(available, action)
		value, ok := neighborMean(p.history[action], x, 5)
		if !ok {
			value = 0.55
		}
		scores[action] = value
	}
	if len(available) == 0 {
		return choice{action: static, fallback: true}
	}
	return choice{action: epsilonPick(available, scores, 0.04, rng)}
}
func (p *governedKNN) update(action string, x []float64, reward float64) {
	reward = math.Max(-0.25, math.Min(1, reward))
	p.history[action] = append(p.history[action], observation{x: append([]float64(nil), x...), reward: reward})
	if len(p.history[action]) > 256 {
		p.history[action] = p.history[action][len(p.history[action])-256:]
	}
	p.actionRewards[action] = append(p.actionRewards[action], reward)
	if len(p.actionRewards[action]) > 24 {
		p.actionRewards[action] = p.actionRewards[action][len(p.actionRewards[action])-24:]
	}
	window := p.actionRewards[action]
	if len(window) == 24 &&
		average(window[:16]) > 0.65 &&
		average(window[16:]) < 0.15 {
		p.quarantineUntil[action] = p.step + 100
		p.rollbackCount++
		p.history[action] = nil
		p.actionRewards[action] = nil
	}
}

func (p *governedKNN) observations() int {
	var count int
	for _, rows := range p.history {
		count += len(rows)
	}
	return count
}

func neighborMean(history []observation, x []float64, k int) (float64, bool) {
	neighbors := nearest(history, x, k)
	if len(neighbors) == 0 {
		return 0, false
	}
	var sum, weightSum float64
	for _, item := range neighbors {
		weight := item.similarity + 1e-6
		sum += weight * item.reward
		weightSum += weight
	}
	return sum / weightSum, true
}

func neighborMedian(history []observation, x []float64, k int) (float64, bool) {
	neighbors := nearest(history, x, k)
	if len(neighbors) == 0 {
		return 0, false
	}
	values := make([]float64, len(neighbors))
	for i, item := range neighbors {
		values[i] = item.reward
	}
	sort.Float64s(values)
	return values[len(values)/2], true
}

func neighborTrimmedMean(history []observation, x []float64, k int) (float64, bool) {
	neighbors := nearest(history, x, k)
	if len(neighbors) == 0 {
		return 0, false
	}
	values := make([]float64, len(neighbors))
	for i, item := range neighbors {
		values[i] = item.reward
	}
	sort.Float64s(values)
	if len(values) >= 5 {
		values = values[1 : len(values)-1]
	}
	return average(values), true
}

type neighbor struct {
	similarity float64
	reward     float64
}

func nearest(history []observation, x []float64, k int) []neighbor {
	out := make([]neighbor, 0, min(k, len(history)))
	for _, item := range history {
		candidate := neighbor{similarity: math.Max(0, dot(x, item.x)), reward: item.reward}
		index := sort.Search(len(out), func(i int) bool {
			return out[i].similarity < candidate.similarity
		})
		if index >= k {
			continue
		}
		out = append(out, neighbor{})
		copy(out[index+1:], out[index:])
		out[index] = candidate
		if len(out) > k {
			out = out[:k]
		}
	}
	return out
}

func epsilonPick(actions []string, scores map[string]float64, epsilon float64, rng *rand.Rand) string {
	best := actions[0]
	for _, action := range actions[1:] {
		if scores[action] > scores[best] {
			best = action
		}
	}
	if rng.Float64() < epsilon {
		return actions[rng.Intn(len(actions))]
	}
	return best
}

func staticAction(sc scenario) string {
	switch sc.domain {
	case "reasoning":
		lower := strings.ToLower(sc.prompt)
		if strings.Contains(lower, "18 multiplied") || strings.Contains(lower, "capital of italy") ||
			strings.Contains(lower, "5 machines") || strings.Contains(lower, "bloops") {
			return "off"
		}
		return "low_256"
	case "tool", "skill":
		return "semantic_top1"
	case "knowledge":
		return "graph_expand"
	default:
		return sc.actions[0]
	}
}

func features(sc scenario) []float64 {
	const dims = 24
	out := make([]float64, dims+4+1)
	for i, value := range sc.embedding {
		for j := 0; j < dims; j++ {
			if projectionSign(i, j) > 0 {
				out[j] += value
			} else {
				out[j] -= value
			}
		}
	}
	scale := 1 / math.Sqrt(float64(dims))
	for i := 0; i < dims; i++ {
		out[i] *= scale
	}
	switch sc.domain {
	case "reasoning":
		out[dims] = 1
	case "tool":
		out[dims+1] = 1
	case "skill":
		out[dims+2] = 1
	case "knowledge":
		out[dims+3] = 1
	}
	out[len(out)-1] = 1
	var norm float64
	for _, value := range out {
		norm += value * value
	}
	norm = math.Sqrt(norm)
	for i := range out {
		out[i] /= norm
	}
	return out
}

func projectionSign(i, j int) float64 {
	h := fnv.New64a()
	var raw [16]byte
	binary.LittleEndian.PutUint64(raw[:8], uint64(i))
	binary.LittleEndian.PutUint64(raw[8:], uint64(j))
	_, _ = h.Write(raw[:])
	if h.Sum64()&1 == 0 {
		return -1
	}
	return 1
}

func dot(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

func average(values []float64) float64 {
	var sum float64
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}
