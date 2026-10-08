package jobsauth

import (
	"context"
	"fmt"

	"github.com/croge130/archipelago/gatehouse-core/evaluation"
	gatehouseFacade "github.com/croge130/archipelago/gatehouse-core/facade"
	jobsFacade "github.com/croge130/archipelago/jobs/facade"
	"github.com/croge130/archipelago/jobs/structure"
	"github.com/google/uuid"
)

// Submit checks that req.RequestedBy may submit to the job's queue and,
// for an assumed-authority job, may cause work to run as req.RunAs, then
// enqueues it. req.RequestedBy is the owner.
//
// The queue is req.QueueKey or, if empty, the task kind's default. The
// requester's cause edge is checked here and again at claim, because
// grants get revoked in between; the executor's edge can only be checked
// at claim, when an executor exists.
func Submit(ctx context.Context, d Deps, req jobsFacade.SubmitRequest) (jobsFacade.SubmitResult, error) {
	if d.JobsWriter == nil {
		return jobsFacade.SubmitResult{}, fmt.Errorf("jobsauth: submit needs a jobs writer")
	}
	if err := requirePrincipal(ctx, d, req.RequestedBy); err != nil {
		return jobsFacade.SubmitResult{}, fmt.Errorf("jobsauth: submit: requester: %w", err)
	}
	def, found, err := d.JobsReader.GetTaskDefinition(ctx, req.TaskKey)
	if err != nil {
		return jobsFacade.SubmitResult{}, fmt.Errorf("jobsauth: submit: %w", err)
	}
	if !found {
		return jobsFacade.SubmitResult{}, fmt.Errorf("%w: %q", jobsFacade.ErrUnknownTask, req.TaskKey)
	}
	if req.QueueKey == "" {
		req.QueueKey = def.DefaultQueueKey
	}
	if err := evaluation.RequireContextPermission(ctx, d.GatehouseReader, req.RequestedBy, PermissionSubmit, ContextTypeQueue, req.QueueKey); err != nil {
		return jobsFacade.SubmitResult{}, fmt.Errorf("jobsauth: submit to queue %q: %w", req.QueueKey, err)
	}

	if req.AuthorityMode == structure.AuthorityAssumed {
		if req.RunAs == nil || *req.RunAs == uuid.Nil {
			return jobsFacade.SubmitResult{}, fmt.Errorf("jobsauth: submit: assumed authority requires RunAs")
		}
		if err := requirePrincipal(ctx, d, *req.RunAs); err != nil {
			return jobsFacade.SubmitResult{}, fmt.Errorf("jobsauth: submit: RunAs: %w", err)
		}
		if err := gatehouseFacade.RequireCanCauseAs(ctx, d.GatehouseReader, req.RequestedBy, *req.RunAs); err != nil {
			return jobsFacade.SubmitResult{}, fmt.Errorf("jobsauth: submit: %w", err)
		}
	}

	res, err := jobsFacade.Submit(ctx, d.JobsReader, d.JobsWriter, req)
	if err != nil {
		return jobsFacade.SubmitResult{}, fmt.Errorf("jobsauth: submit: %w", err)
	}
	return res, nil
}

func requirePrincipal(ctx context.Context, d Deps, id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: nil principal ID", ErrPrincipalNotFound)
	}
	_, found, err := d.GatehouseReader.GetPrincipal(ctx, id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrPrincipalNotFound, id)
	}
	return nil
}
