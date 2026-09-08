package agui

const swarmCoordinatorEventName = "aura.swarm.coordinator"

type coordinatorRunStatus struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
}

// The lingering parent session also announces a fast completed continuation
// that began and ended between two worker-stream observations.
func (r *RunRegistry) coordinatorStatus(owner, conv string) coordinatorRunStatus {
	if r == nil {
		return coordinatorRunStatus{}
	}
	r.mu.Lock()
	sess := r.latestByThread[threadKey{identity: owner, thread: conv}]
	r.mu.Unlock()
	if sess == nil {
		return coordinatorRunStatus{}
	}
	status := "running"
	if done, _ := sess.terminalState(); done {
		status = "finished"
	}
	return coordinatorRunStatus{RunID: sess.RunID, Status: status}
}
