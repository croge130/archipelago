package jobsauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/google/uuid"
)

// canAccess reports whether principal may act on job with permissionKey:
// always for the job's owner, otherwise by the permission on the job's
// queue context. A denial is (false, nil); only a store failure is an
// error.
func canAccess(ctx context.Context, d Deps, principal uuid.UUID, job structure.Job, permissionKey string) (bool, error) {
	if job.RequestedBy == principal {
		return true, nil
	}
	err := evaluation.RequireContextPermission(ctx, d.GatehouseReader, principal, permissionKey, ContextTypeQueue, job.QueueKey)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, evaluation.ErrDenied):
		return false, nil
	default:
		return false, err
	}
}

// GetJob returns a job to a principal allowed to read it: its owner, or
// anyone holding jobs.read on its queue.
func GetJob(ctx context.Context, d Deps, principal, jobID uuid.UUID) (structure.Job, error) {
	job, found, err := d.JobsReader.GetJob(ctx, jobID)
	if err != nil {
		return structure.Job{}, fmt.Errorf("jobsauth: get job: %w", err)
	}
	if !found {
		return structure.Job{}, jobsFacade.ErrJobNotFound
	}
	ok, err := canAccess(ctx, d, principal, job, PermissionRead)
	if err != nil {
		return structure.Job{}, fmt.Errorf("jobsauth: get job: %w", err)
	}
	if !ok {
		return structure.Job{}, fmt.Errorf("jobsauth: get job: %w: %s may not read jobs in queue %q", evaluation.ErrDenied, principal, job.QueueKey)
	}
	return job, nil
}

// ListJobs returns the jobs matching f that principal may read: its own,
// and those in queues it holds jobs.read on. Each queue is checked once.
// f.Limit applies before this filtering, so fewer jobs than the limit
// may come back.
func ListJobs(ctx context.Context, d Deps, principal uuid.UUID, f structure.ListFilter) ([]structure.Job, error) {
	jobs, err := d.JobsReader.ListJobs(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("jobsauth: list jobs: %w", err)
	}
	readable := map[string]bool{}
	var out []structure.Job
	for _, j := range jobs {
		if j.RequestedBy == principal {
			out = append(out, j)
			continue
		}
		ok, seen := readable[j.QueueKey]
		if !seen {
			if ok, err = canAccess(ctx, d, principal, j, PermissionRead); err != nil {
				return nil, fmt.Errorf("jobsauth: list jobs: %w", err)
			}
			readable[j.QueueKey] = ok
		}
		if ok {
			out = append(out, j)
		}
	}
	return out, nil
}

// Cancel requests cancellation by a principal allowed to: the owner, or
// anyone holding jobs.cancel on the job's queue. Cancellation is
// cooperative — see jobs/facade.Cancel.
func Cancel(ctx context.Context, d Deps, principal, jobID uuid.UUID) (structure.CancelResult, error) {
	if d.JobsWriter == nil {
		return structure.CancelResult{}, fmt.Errorf("jobsauth: cancel needs a jobs writer")
	}
	job, found, err := d.JobsReader.GetJob(ctx, jobID)
	if err != nil {
		return structure.CancelResult{}, fmt.Errorf("jobsauth: cancel: %w", err)
	}
	if !found {
		return structure.CancelResult{}, jobsFacade.ErrJobNotFound
	}
	ok, err := canAccess(ctx, d, principal, job, PermissionCancel)
	if err != nil {
		return structure.CancelResult{}, fmt.Errorf("jobsauth: cancel: %w", err)
	}
	if !ok {
		return structure.CancelResult{}, fmt.Errorf("jobsauth: cancel: %w: %s may not cancel jobs in queue %q", evaluation.ErrDenied, principal, job.QueueKey)
	}
	return jobsFacade.Cancel(ctx, d.JobsReader, d.JobsWriter, jobID)
}
