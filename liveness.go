package tessera

import "github.com/aalpar/crdt/dotcontext"

// StalenessTracker detects ring members that have stopped producing
// BlockRef events. It observes the local BlockRef causal context on
// each sync round: if a member's max sequence number hasn't advanced
// after a configurable number of rounds, it's reported as stale.
//
// This is protocol-level state, not a CRDT. Each worker maintains its
// own tracker independently. Different workers may reach different
// staleness conclusions at different times — that's fine with "any peer
// can evict" semantics.
type StalenessTracker struct {
	lastSeen    map[string]uint64 // member → last observed max seq
	staleRounds map[string]int    // member → consecutive rounds with no advancement
	threshold   int               // rounds before reporting stale
}

// NewStalenessTracker creates a tracker that reports a member as stale
// after threshold consecutive sync rounds with no new events.
func NewStalenessTracker(threshold int) *StalenessTracker {
	return &StalenessTracker{
		lastSeen:    make(map[string]uint64),
		staleRounds: make(map[string]int),
		threshold:   threshold,
	}
}

// Observe checks each ring member's activity in the BlockRef causal
// context. Call after each sync round. Returns member IDs that have
// exceeded the staleness threshold.
//
// A member whose replica ID has never appeared in the causal context
// (max == 0) is not tracked — they joined but haven't produced events
// yet, and an idle member with no events cannot block GC dominance.
func (s *StalenessTracker) Observe(ring *Ring, ctx *dotcontext.CausalContext) []string {
	var stale []string
	for _, member := range ring.Members() {
		id := dotcontext.ReplicaID(member)
		currentMax := ctx.Max(id)

		// Skip members with no events — they can't block GC.
		if currentMax == 0 {
			continue
		}

		if prev, ok := s.lastSeen[member]; ok && currentMax == prev {
			s.staleRounds[member]++
		} else {
			s.staleRounds[member] = 0
		}
		s.lastSeen[member] = currentMax

		if s.staleRounds[member] >= s.threshold {
			stale = append(stale, member)
		}
	}
	return stale
}

// Reset clears staleness tracking for a member (e.g. after eviction).
func (s *StalenessTracker) Reset(member string) {
	delete(s.lastSeen, member)
	delete(s.staleRounds, member)
}
