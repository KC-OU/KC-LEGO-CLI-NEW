package sync

// AggregateStock sums lot amounts per part id, matching the original's
// part_stock_map. A part with no lots still gets a zero entry as long as
// its id appears in activePartIDs, so every active part gets a stock row.
func AggregateStock(activePartIDs []int, lotPartIDs []int, lotAmounts []float64) map[int]int {
	sums := make(map[int]float64, len(activePartIDs))
	for _, id := range activePartIDs {
		sums[id] = 0
	}
	for i, partID := range lotPartIDs {
		if _, active := sums[partID]; active {
			sums[partID] += lotAmounts[i]
		}
	}
	out := make(map[int]int, len(sums))
	for id, amt := range sums {
		out[id] = int(amt)
	}
	return out
}

// SoftDeleteTargets returns the existing ids that are no longer in
// activeIDs, mirroring the original's "anything not in active_*_ids gets
// is_valid=0" diff pass for both categories and SPUs.
func SoftDeleteTargets(existingIDs, activeIDs []int) []int {
	active := make(map[int]bool, len(activeIDs))
	for _, id := range activeIDs {
		active[id] = true
	}
	// Always non-nil: this gets json.Marshal'd straight into the generated
	// Python script's payload, and a nil slice serializes as `null`, which
	// crashes the script's `for x in data["..."]:` loop when there's nothing
	// to soft-delete (the common case) instead of iterating zero times.
	out := []int{}
	for _, id := range existingIDs {
		if !active[id] {
			out = append(out, id)
		}
	}
	return out
}
