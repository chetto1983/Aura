package main

import (
	"encoding/binary"
	"hash/fnv"
	"math"
)

const projectionDim = 24

func contextFeatures(sc benchScenario, profile string) []float64 {
	out := make([]float64, projectionDim+4+2+1)
	for i, value := range sc.Embedding {
		for j := 0; j < projectionDim; j++ {
			if projectionSign(i, j) > 0 {
				out[j] += value
			} else {
				out[j] -= value
			}
		}
	}
	scale := 1 / math.Sqrt(float64(projectionDim))
	for i := 0; i < projectionDim; i++ {
		out[i] *= scale
	}
	domainOffset := projectionDim
	switch sc.Domain {
	case "reasoning":
		out[domainOffset] = 1
	case "tool":
		out[domainOffset+1] = 1
	case "skill":
		out[domainOffset+2] = 1
	case "knowledge":
		out[domainOffset+3] = 1
	}
	switch profile {
	case "quality_first":
		out[domainOffset+4], out[domainOffset+5] = 0.02, 0.01
	case "balanced":
		out[domainOffset+4], out[domainOffset+5] = 0.08, 0.04
	case "economy":
		out[domainOffset+4], out[domainOffset+5] = 0.18, 0.08
	}
	out[len(out)-1] = 1
	return normalize(out)
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

func normalize(values []float64) []float64 {
	var norm float64
	for _, value := range values {
		norm += value * value
	}
	if norm == 0 {
		return values
	}
	norm = math.Sqrt(norm)
	out := make([]float64, len(values))
	for i, value := range values {
		out[i] = value / norm
	}
	return out
}

func dot(a, b []float64) float64 {
	var sum float64
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}
