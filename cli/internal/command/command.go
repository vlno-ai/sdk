// Package command implements the customer CLI over the versioned worker API.
package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/vlno-ai/sdk/cli/internal/api"
	"io"
	"strings"
	"time"
)

// An evaluation outcome is already printed as a report, not an API error.
var evaluationUnpassed = errors.New("evaluation_not_passed")

var executionUnpassed = errors.New("execution_not_passed")

type failure struct {
	Code    string `json:"code"`
	Status  int    `json:"status,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Stage   string `json:"stage,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	WorldID string `json:"world_id,omitempty"`
	JobID   string `json:"job_id,omitempty"`
	RunID   string `json:"run_id,omitempty"`
	Key     string `json:"idempotency_key,omitempty"`
}

func (f *failure) Error() string { return f.Code }

func fail(code string) error { return &failure{Code: code} }

type options struct {
	endpoint, caFile, configPath string
	json                         bool
	timeout, waitTimeout         time.Duration
}

type runner struct {
	ctx      context.Context
	in       io.Reader
	out, err io.Writer
	opts     options
}

// Run returns an exit status; diagnostics contain no credentials or raw server errors.
func Run(ctx context.Context, args []string, in io.Reader, out, stderr io.Writer) int {
	opts, remaining, err := globals(args)
	r := runner{ctx: ctx, in: in, out: out, err: stderr, opts: opts}
	if err == nil {
		err = r.execute(remaining)
	}
	if err == nil {
		return 0
	}
	if errors.Is(err, evaluationUnpassed) || errors.Is(err, executionUnpassed) {
		return 1
	}
	f := &failure{Code: "command_failed"}
	var own *failure
	var remote *api.Error
	if errors.As(err, &own) {
		f = own
	} else if errors.As(err, &remote) {
		f = &failure{Code: remote.Code, Status: remote.Status, Outcome: remote.Outcome, Reason: remote.Reason, Stage: remote.Stage}
	}
	if errors.Is(err, context.Canceled) {
		f.Code = "cancelled"
	}
	if opts.json {
		_ = json.NewEncoder(stderr).Encode(map[string]any{"error": f})
	} else {
		fmt.Fprintln(stderr, "Error:", f.Code)
		if f.WorldID != "" {
			fmt.Fprintln(stderr, "World:", f.WorldID)
		}
		if f.RunID != "" {
			fmt.Fprintln(stderr, "Run:", f.RunID)
		}
		if f.JobID != "" {
			fmt.Fprintln(stderr, "Job:", f.JobID)
		}
		if f.Key != "" {
			fmt.Fprintln(stderr, "Idempotency key:", f.Key)
		}
		if f.Stage != "" {
			fmt.Fprintln(stderr, "Stage:", f.Stage)
		}
		if f.Reason != "" {
			fmt.Fprintln(stderr, "Reason:", f.Reason)
		}
		if f.Outcome != "" {
			fmt.Fprintln(stderr, "Outcome:", f.Outcome)
		}
	}
	if f.Code == "cancelled" {
		return 130
	}
	if strings.HasPrefix(f.Code, "invalid_") || f.Code == "usage" || f.Code == "unsupported_feature" {
		return 2
	}
	return 1
}
