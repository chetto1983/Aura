package adaptive

import "maps"

func canonicalAssignment(assignment Assignment) Assignment {
	assignment.EligibleActions = append([]string(nil), assignment.EligibleActions...)
	assignment.ActionProbabilities = append(
		[]ActionProbability(nil),
		assignment.ActionProbabilities...,
	)
	for i := range assignment.ActionProbabilities {
		assignment.ActionProbabilities[i].Probability =
			canonicalZero(assignment.ActionProbabilities[i].Probability)
	}
	features := make(map[FeatureKey]float64, len(assignment.Features))
	for key, value := range assignment.Features {
		features[key] = canonicalZero(value)
	}
	assignment.Features = features
	return assignment
}

func canonicalDelivery(delivery Delivery) Delivery {
	delivery.ResultIDs = append([]ResultID{}, delivery.ResultIDs...)
	delivery.Revisions = delivery.Revisions.clone()
	limits := make(map[string]int, len(delivery.EffectiveLimits))
	maps.Copy(limits, delivery.EffectiveLimits)
	delivery.EffectiveLimits = limits
	return delivery
}

func canonicalZero(value float64) float64 {
	if value == 0 {
		return 0
	}
	return value
}
