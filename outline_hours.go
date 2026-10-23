package hours

import "sort"

// rebalanceOutlineHours treats model hours as relative weights. Arithmetic is
// owned by the service, so the proposed activities and final assessment sum
// exactly to the requested volume before the methodist reviews the outline.
func rebalanceOutlineHours(out *GeneratedOutline, requested Hours) bool {
	requestedMinutes, err := requested.minutes()
	if err != nil || requestedMinutes <= 0 {
		return false
	}
	finalMinutes, err := out.FinalAttestation.minutes()
	if err != nil {
		return false
	}
	if finalMinutes <= 0 || finalMinutes >= requestedMinutes {
		finalMinutes = 4 * 60
		if requestedMinutes < 32*60 {
			finalMinutes = 2 * 60
		}
		if finalMinutes >= requestedMinutes {
			return false
		}
	}
	type topicRef struct {
		module, topic int
		weight        int64
		activityCount int64
	}
	var refs []topicRef
	var totalMinutes int64 = finalMinutes
	var activityCount int64
	var hasZeroHours bool
	for mi := range out.Modules {
		for ti := range out.Modules[mi].Topics {
			topic := &out.Modules[mi].Topics[ti]
			if len(topic.Activities) == 0 {
				return false
			}
			ref := topicRef{module: mi, topic: ti, activityCount: int64(len(topic.Activities))}
			activityCount += ref.activityCount
			for _, activity := range topic.Activities {
				minutes, err := activity.Hours.minutes()
				if err != nil || minutes > 1_000_000 {
					return false
				}
				if minutes == 0 {
					hasZeroHours = true
				}
				ref.weight += minutes
				totalMinutes += minutes
			}
			refs = append(refs, ref)
		}
	}
	// Preserve the per-topic budget supplied to the model when it returned
	// the requested layout. Other layouts retain the general weight allocator.
	plannedFinal, plannedTopics := plannedOutlineHours(requested)
	usePlan := len(plannedTopics) > 0 && len(out.Modules)*2 == len(plannedTopics) && len(refs) == len(plannedTopics)
	for _, module := range out.Modules {
		usePlan = usePlan && len(module.Topics) == 2
	}
	planMismatch := false
	if usePlan {
		finalMinutes, _ = plannedFinal.minutes()
		for i, ref := range refs {
			budget, _ := plannedTopics[i].minutes()
			planMismatch = planMismatch || ref.weight != budget
		}
	}
	if len(refs) == 0 || (totalMinutes == requestedMinutes && !hasZeroHours && !planMismatch && Hours(float64(finalMinutes)/60) == out.FinalAttestation) {
		return false
	}
	targetMinutes := requestedMinutes - finalMinutes
	var unit int64
	for _, candidate := range []int64{60, 30, 15, 5, 1} {
		fits := targetMinutes%candidate == 0 && targetMinutes/candidate >= activityCount
		if usePlan {
			for i, ref := range refs {
				budget, _ := plannedTopics[i].minutes()
				fits = fits && budget%candidate == 0 && budget/candidate >= ref.activityCount
			}
		}
		if fits {
			unit = candidate
			break
		}
	}
	if unit == 0 {
		return false
	}
	topicUnits := targetMinutes / unit
	weights := make([]int64, len(refs))
	minimums := make([]int64, len(refs))
	for i, ref := range refs {
		weights[i] = ref.weight
		minimums[i] = ref.activityCount
	}
	allocation := weightedHours(topicUnits, weights, minimums)
	if usePlan {
		for i, budget := range plannedTopics {
			minutes, _ := budget.minutes()
			allocation[i] = minutes / unit
		}
	}
	for i, ref := range refs {
		topic := &out.Modules[ref.module].Topics[ref.topic]
		activityWeights := make([]int64, len(topic.Activities))
		for ai, activity := range topic.Activities {
			activityWeights[ai], _ = activity.Hours.minutes()
		}
		activityMinimums := make([]int64, len(topic.Activities))
		for ai := range activityMinimums {
			activityMinimums[ai] = 1
		}
		parts := weightedHours(allocation[i], activityWeights, activityMinimums)
		for ai, part := range parts {
			topic.Activities[ai].Hours = Hours(float64(part*unit) / 60)
		}
	}
	out.FinalAttestation = Hours(float64(finalMinutes) / 60)
	return true
}

func weightedHours(total int64, rawWeights, minimums []int64) []int64 {
	result := make([]int64, len(rawWeights))
	remaining := total
	var weightSum int64
	weights := make([]int64, len(rawWeights))
	for i, weight := range rawWeights {
		if weight <= 0 {
			weight = 1
		}
		weights[i] = weight
		weightSum += weight
		result[i] = minimums[i]
		remaining -= minimums[i]
	}
	if remaining == 0 {
		return result
	}
	remainders := make([]int64, len(result))
	var allocated int64
	for i, weight := range weights {
		share := remaining * weight
		result[i] += share / weightSum
		remainders[i] = share % weightSum
		allocated += share / weightSum
	}
	order := make([]int, len(result))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return remainders[order[i]] > remainders[order[j]] })
	for i := int64(0); i < remaining-allocated; i++ {
		result[order[i]]++
	}
	return result
}
