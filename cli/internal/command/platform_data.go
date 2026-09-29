package command

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

func validDataPolicy(value map[string]any) bool {
	days, exists := value["retentionDays"]
	if !exists || (days != nil && days != json.Number("7") && days != json.Number("30") && days != json.Number("90")) {
		return false
	}
	revision, ok := value["revision"].(json.Number)
	if !ok {
		return false
	}
	n, err := revision.Int64()
	return err == nil && n >= 0 && value["appliesTo"] == "new_runs" && value["retentionStartsAt"] == "run_terminal" && value["externalCopiesManaged"] == false
}

func validDataStatus(value map[string]any, id string) bool {
	run, ok := value["runId"].(string)
	if !ok || !productRunID.MatchString(run) || (id != "" && run != id) || value["externalCopiesManaged"] != false || value["backupDeletionIndividuallyVerified"] != false {
		return false
	}
	if _, ok := value["canRequestDeletion"].(bool); !ok {
		return false
	}
	for _, field := range []string{"expiresAt", "requestedAt", "workerDeletedAt", "archiveDeletedAt", "databaseDeletedAt", "backupExpiryEstimate"} {
		raw, exists := value[field]
		if !exists {
			return false
		}
		if raw == nil {
			continue
		}
		stamp, ok := raw.(string)
		if !ok {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
			return false
		}
	}
	switch value["state"] {
	case "retained":
	case "deleting", "live_data_deleted":
		if value["requestedAt"] == nil || value["canRequestDeletion"] != false {
			return false
		}
		if value["state"] == "live_data_deleted" && (value["workerDeletedAt"] == nil || value["archiveDeletedAt"] == nil || value["databaseDeletedAt"] == nil) {
			return false
		}
	default:
		return false
	}
	return true
}

func (r *runner) productData(args []string) error {
	if len(args) == 0 {
		return fail("usage")
	}
	action, path, method := args[0], "/v1/world-data/policy", "GET"
	var body any
	var id string
	limit, offset := 50, 0
	switch action {
	case "policy":
		if len(args) != 1 {
			return fail("usage")
		}
	case "set-policy":
		fs := flags("platform data set-policy")
		days := fs.String("days", "", "")
		revision := fs.Int("revision", -1, "")
		if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *revision < 0 {
			return fail("usage")
		}
		var retention any
		switch *days {
		case "keep":
			retention = nil
		case "7", "30", "90":
			retention, _ = strconv.Atoi(*days)
		default:
			return fail("invalid_retention")
		}
		method, body = "PUT", map[string]any{"retentionDays": retention, "expectedRevision": *revision}
	case "status", "delete":
		if len(args) < 2 {
			return fail("usage")
		}
		id = args[1]
		if !productRunID.MatchString(id) {
			return fail("invalid_run_id")
		}
		path = "/v1/world-runs/" + id + "/data"
		if action == "delete" {
			fs := flags("platform data delete")
			confirm := fs.String("confirm-run", "", "")
			if fs.Parse(args[2:]) != nil || fs.NArg() != 0 || *confirm != id {
				return fail("confirmation_required")
			}
			path, method, body = path+"/deletion", "POST", map[string]any{"confirmRunId": id}
		} else if len(args) != 2 {
			return fail("usage")
		}
	case "deletions":
		fs := flags("platform data deletions")
		fs.IntVar(&limit, "limit", 50, "")
		fs.IntVar(&offset, "offset", 0, "")
		if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
			return fail("usage")
		}
		path = "/v1/world-data/deletions?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(offset)
	default:
		return fail("usage")
	}
	settings, err := r.settings()
	if err != nil {
		return err
	}
	if !customerKey.MatchString(settings.APIKey) {
		return fail("invalid_customer_key")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.ctx, r.opts.waitTimeout)
	defer cancel()
	value, err := c.Request(ctx, method, path, body, "")
	if err != nil {
		return err
	}
	switch action {
	case "policy", "set-policy":
		if !validDataPolicy(value) {
			return fail("invalid_data_policy")
		}
	case "status", "delete":
		if !validDataStatus(value, id) {
			return fail("invalid_data_status")
		}
	case "deletions":
		items, ok := value["items"].([]any)
		next, exists := value["nextOffset"]
		if !ok || len(items) > limit || value["limit"] != json.Number(strconv.Itoa(limit)) || value["offset"] != json.Number(strconv.Itoa(offset)) || !exists || (next != nil && next != json.Number(strconv.Itoa(offset+limit))) {
			return fail("invalid_data_deletions")
		}
		for _, item := range items {
			entry, ok := item.(map[string]any)
			if !ok || !validDataStatus(entry, "") {
				return fail("invalid_data_status")
			}
		}
	}
	return r.emit(value)
}
