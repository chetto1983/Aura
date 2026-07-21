package cron

import "github.com/chetto1983/aura/internal/obs"

const schedulerMeterScope = "github.com/chetto1983/aura/internal/cron"

var schedulerLifecycleBoundary = obs.NewGlobalBoundary(schedulerMeterScope, obs.BoundaryConfig{
	Operation: "scheduler_tick", State: "running", Count: obs.SchedulerTransitionsID,
})

var schedulerScanBoundary = obs.NewGlobalBoundary(schedulerMeterScope, obs.BoundaryConfig{
	Operation: "scheduler_tick", Count: obs.SchedulerRunsID, Duration: obs.SchedulerDurationID,
})

var schedulerClaimBoundary = obs.NewGlobalBoundary(schedulerMeterScope, obs.BoundaryConfig{
	Operation: "scheduler_claim", State: "pending",
	Count: obs.SchedulerTransitionsID, Duration: obs.SchedulerDurationID,
})

var schedulerJobBoundary = obs.NewGlobalBoundary(schedulerMeterScope, obs.BoundaryConfig{
	Operation: "scheduler_job", ToolClass: obs.ToolClassScheduler, State: "in_progress",
	Count: obs.SchedulerRunsID, Duration: obs.SchedulerDurationID,
})
