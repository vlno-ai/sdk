package command

func (r *runner) create(args []string) error {
	fs := flags("create")
	app := fs.String("app", "", "")
	template := fs.String("template", "", "")
	fixture := fs.String("fixture", "", "")
	environment := fs.Bool("environment", false, "")
	ttl := fs.Int("ttl", 900, "")
	mapping := fs.String("mapping", "", "")
	supplied := fs.String("idempotency-key", "", "")
	noWait := fs.Bool("no-wait", false, "")
	if fs.Parse(args) != nil || fs.NArg() != 0 || (*app == "") == (*template == "") {
		return fail("usage")
	}
	if *fixture != "" && *template == "" {
		return fail("invalid_fixture")
	}
	if (*template != "" && !versionRef.MatchString(*template)) || (*fixture != "" && !versionRef.MatchString(*fixture)) {
		return fail("invalid_version_ref")
	}
	if *ttl < 60 || *ttl > 3600 {
		return fail("invalid_lease")
	}
	key, e := replayKey(*supplied)
	if e != nil {
		return e
	}
	payload := map[string]any{"ttl_seconds": *ttl}
	if *environment {
		payload["environment"] = true
	}
	if *app != "" {
		payload["image"] = *app
	} else {
		payload["template"] = *template
	}
	if *fixture != "" {
		payload["fixture"] = *fixture
	}
	if *mapping != "" {
		v, e := r.readObject(*mapping)
		if e != nil {
			return e
		}
		payload["mapping"] = v
	}
	c, e := r.client()
	if e != nil {
		return e
	}
	r.progress("Idempotency key:", key)
	v, e := c.Request(r.ctx, "POST", "/v1/worlds", payload, key)
	if e != nil {
		return lifecycleError(e, "", key)
	}
	id, ok := v["id"].(string)
	if !ok || !worldID.MatchString(id) {
		return lifecycleError(fail("invalid_response"), "", key)
	}
	r.progress("World:", id)
	if !*noWait {
		v, e = r.wait(c, id, "ready", v)
		if e != nil {
			return lifecycleError(e, id, key)
		}
	}
	if key != "" {
		v["idempotency_key"] = key
	}
	return r.emit(public(v))
}
