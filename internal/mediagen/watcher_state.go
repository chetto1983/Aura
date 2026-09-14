package mediagen

// jobHandoff decides who handles a tracked job's terminal outcome: the inline waiter that
// registered before waiting, or the wake path that notifies the conversation. Exactly one of
// them gets it. Every method runs under the watcher's mutex; the caller notifies after
// unlocking.
type jobHandoff struct {
	waiting, terminal, claimed, notified bool
}

// finish records the terminal outcome and reports whether the wake path must notify.
func (s *jobHandoff) finish() bool {
	s.terminal = true
	return s.notifyIfDetached()
}

// detach unregisters the inline waiter and reports whether the wake path must notify.
func (s *jobHandoff) detach() bool {
	s.waiting = false
	return s.notifyIfDetached()
}

// claim gives the still-registered inline waiter the finished job.
func (s *jobHandoff) claim() bool {
	if !s.terminal || !s.waiting || s.claimed || s.notified {
		return false
	}
	s.claimed = true
	s.waiting = false
	return true
}

// release returns a claim whose inline delivery failed before its database claim, and
// reports whether the wake path must notify instead. Only a caller that knows the database
// claim did not complete may call it.
func (s *jobHandoff) release() bool {
	s.claimed = false
	return s.detach()
}

// settled reports a handoff with nothing left to decide: the job finished and no waiter is
// registered for it.
func (s *jobHandoff) settled() bool {
	return s.terminal && !s.waiting
}

func (s *jobHandoff) notifyIfDetached() bool {
	if !s.terminal || s.waiting || s.claimed || s.notified {
		return false
	}
	s.notified = true
	return true
}
