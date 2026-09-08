package swarm

import (
	"context"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/steer"
)

func openWorkerControl(ctx context.Context, rc RunConfig, childID string, cancel context.CancelCauseFunc) (agent.WorkerControlSession, agent.SteerInbox, error) {
	if rc.Controls == nil {
		return agent.WorkerControlSession{}, nil, nil
	}
	var inbox *steer.WorkerInbox
	control, err := rc.Controls.StartWorker(ctx, agent.WorkerControlParams{
		ConversationID: rc.ConvID, ChildID: childID, SteerEnabled: rc.Steer != nil,
		Cancel: func() { cancel(context.Canceled) },
		Stop: func(controlCtx context.Context) error {
			if rc.RecordCancellation != nil {
				if err := rc.RecordCancellation(controlCtx, childID); err != nil {
					return err
				}
			}
			cancel(agent.ErrWorkerStopped)
			return nil
		},
		OnClose: func() error { return inbox.Close() },
	})
	if err != nil {
		return agent.WorkerControlSession{}, nil, err
	}
	if rc.Steer == nil {
		return control, nil, nil
	}
	inbox, err = rc.Steer.ForWorker(ctx, rc.ConvID, childID, control.RunID)
	if err != nil {
		_ = control.Finish()
		return agent.WorkerControlSession{}, nil, err
	}
	return control, inbox, nil
}
