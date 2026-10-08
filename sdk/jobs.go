package sdk

import (
	"fmt"

	"github.com/croge130/archipelago/jobsauth"
)

// JobsAuth assembles the stores jobsauth works over from this App's own,
// so a caller does not have to repeat the wiring. It is available when
// both the Jobs and Gatehouse modes are enabled — jobsauth is the
// integration of those two bases, and a node holding only the jobs base
// uses its facade directly. Writers are passed through as they are: a
// read-only node gets a Deps with nil writers, and jobsauth's operations
// that write fail clearly on it instead of panicking.
func (a *App) JobsAuth() (jobsauth.Deps, error) {
	if !a.Modes.Jobs || !a.Modes.Gatehouse {
		return jobsauth.Deps{}, fmt.Errorf("sdk: JobsAuth needs both the Jobs and Gatehouse modes enabled")
	}
	return jobsauth.Deps{
		GatehouseReader: a.Gatehouse.Reader,
		GatehouseWriter: a.Gatehouse.Writer,
		JobsReader:      a.Jobs.Reader,
		JobsWriter:      a.Jobs.Writer,
	}, nil
}
