package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

type (
	// Check holds information about the actual health-check.
	Check struct {

		// The unique name of the check.
		Name string

		// The actual health-check function
		Handler func(context.Context) error

		// The interval to query the healthfunc
		Interval time.Duration

		// The timeout for the healthfunc duration
		Timeout time.Duration

		// Aspect to process the result
		Aspect func(Check, error) error
	}

	// Doctor encapsulates all the health functionality
	Doctor struct {
		checks *healthChecks
		status *healthStatus
	}
)

// internal types
type (
	// healthStatus wraps the original check with internal fields to hold state
	healthCheckStatus struct {
		Check
		healthy bool
		msg     string
		pos     uint
		sync.RWMutex
	}

	// healthChecks is a sync-list of all health-states
	healthChecks struct {
		sync.RWMutex
		items map[string]*healthCheckStatus
	}

	// HealthStatus holds the status of all the healthchecks
	healthStatus struct {
		sync.RWMutex
		status uint64
	}
)

// NewDoctor creates a new doctor
func NewDoctor() *Doctor {
	return &Doctor{
		checks: &healthChecks{items: make(map[string]*healthCheckStatus)},
		status: &healthStatus{status: 0},
	}
}

// Investigate checks if a certain check is good or not. The health-check should not block and may not take
// longer than its timeout to finish.
func (health *Doctor) Investigate(ctx context.Context, healthCheck *Check) error {
	health.checks.Lock()
	defer health.checks.Unlock()
	pos := uint(len(health.checks.items))
	if pos < 63 {
		check := &healthCheckStatus{
			Check:   *healthCheck,
			healthy: false,
			msg:     "[n/a]",
			pos:     pos,
		}
		health.checks.items[healthCheck.Name] = check
		health.status.update(pos, false)
		go check.start(ctx, health.status)
	} else {
		return errors.New("health-check treshold (64) exceeded")
	}
	return nil
}

// Healthy return if the service is healty or not (true/false)
func (health *Doctor) Healthy() bool {
	health.status.RLock()
	defer health.status.RUnlock()
	return health.status.status == 0
}

// start the health check. We use the time.After method instead of Tick to avoid
// having a stack overflow when health-check do not end in a timely manner
func (hc *healthCheckStatus) start(ctx context.Context, status *healthStatus) {
	check := func() {
		subctx, cancel := context.WithTimeout(ctx, hc.Timeout)
		go func() {
			defer cancel()
			err := hc.Handler(subctx)
			if hc.Aspect != nil {
				err = hc.Aspect(hc.Check, err)
			}
			hc.Lock()
			defer hc.Unlock()
			if err == nil {
				status.update(hc.pos, true)
				hc.healthy = true
				hc.msg = ""
			} else {
				status.update(hc.pos, false)
				hc.healthy = false
				hc.msg = err.Error()
			}

		}()
		<-subctx.Done()
	}

	for {
		check()

		select {
		case <-ctx.Done():
			return
		case <-time.After(hc.Interval):
			continue
		}
	}
}

// update the health check status on a given position
func (c *healthStatus) update(pos uint, value bool) {
	c.Lock()
	defer c.Unlock()
	if !value {
		c.status |= (1 << pos)
	} else {
		c.status &= ^(1 << pos)
	}
}

// Handler renders the health status page
func (health *Doctor) Handler(w http.ResponseWriter, r *http.Request) {
	var status = struct {
		Status string            `json:"status"`
		Errors map[string]string `json:"errors,omitempty"`
	}{}

	var statusCode int
	func() {
		health.status.RLock()
		defer health.status.RUnlock()
		if health.status.status == 0 {
			statusCode = http.StatusOK
			status.Status = "up"
		} else {
			statusCode = http.StatusServiceUnavailable
			status.Status = "down"
			status.Errors = health.checks.failing()
		}
	}()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(status)
}

// make a map with failing health checks
func (checks *healthChecks) failing() map[string]string {
	checks.RLock()
	defer checks.RUnlock()
	errors := make(map[string]string)
	for name, hc := range checks.items {
		hc.Lock()
		defer hc.Unlock()
		if len(hc.msg) > 0 {
			errors[name] = hc.msg
		}
	}
	return errors
}
