package command

const Version = "0.14.0"

const help = `vlno — remote world environments (preview)

Usage:
  vlno login --key-stdin
  vlno platform login --key-stdin
  vlno platform assessments prepare <assessment-id> --revision <scope-revision-id>
  vlno platform assessments admit <assessment-id> --revision <scope-revision-id> --approval <review-id> --system-revision <system-revision-id> --idempotency-key <saved-key>
  vlno platform assessments status <cw_run-id>
  vlno platform data policy
  vlno platform data set-policy --days <7|30|90|keep> --revision <current-revision>
  vlno platform data status <cw_run-id>
  vlno platform data delete <cw_run-id> --confirm-run <same-cw_run-id>
  vlno platform data deletions [--limit 50] [--offset 0]
  vlno platform suites list
  vlno platform runs create <suite-ref> --revision <label> --idempotency-key <key> [--environment] [--ttl 1800]
  vlno platform runs status <cw_run-id>
  vlno platform runs next <cw_run-id> --claim-file <private-file> --out <new-private-file>
  vlno platform runs finish <cw_run-id> --case <index> --claim-file <private-file> [--agent-status completed]
  vlno platform runs wait <cw_run-id>
  vlno platform runs watch <cw_run-id> [--after <cursor>]
  vlno platform runs cancel <cw_run-id>
  vlno platform runs transcript <cw_run-id> --case <index> [--after <cursor>]
  vlno platform runs record <cw_run-id> --case <index> --claim-file <private-file>
  vlno platform runs evidence <cw_run-id>
  vlno platform runs artifact <cw_run-id> --case <index> --kind <report|snapshot|evidence|trace> --out <new-private-file>
  vlno logout
  vlno apps validate --file <app.json>
  vlno apps import --file <app.json> [--idempotency-key <key>] [--no-wait]
  vlno apps status <job-id>
  vlno suites run <suite-ref> --revision <label> (--agent-config <file> | --harness <alias>) [--environment] [--case-timeout 300s]
  vlno runs status <run-id>
  vlno runs cancel <run-id>
  vlno runs compare <baseline-id> <candidate-id>
  vlno catalog harnesses [show <alias>]
  vlno catalog apps [show <app>]
  vlno catalog worlds [show <ref>] [--app <app>]
  vlno catalog scenarios [show <ref>] [--world <ref>]
  vlno catalog families [show <family>]
  vlno catalog suites [show <ref>]
  vlno scenarios generate --family <family> --template <ref> --seed <integer>
  vlno scenarios status <job-id>
  vlno scenarios freeze <job-id> --suite <ref>
  vlno authoring create --file <brief.json> [--idempotency-key <key>]
  vlno authoring status <job-id>
  vlno authoring context <job-id>
  vlno authoring run <job-id> [--no-wait]
  vlno authoring live <job-id> [--no-wait]
  vlno authoring validate <job-id> [--no-wait]
  vlno authoring freeze <job-id> --suite <ref>
  vlno authoring role <job-id> --role <role> --provider <name> --model <model> --session <id> --token-file <new-file>
  vlno authoring submit <job-id> --file <artifact.json> --basis <sha256>
  vlno worlds create --app <app> [--ttl 900] [--mapping <file>]
  vlno worlds create --template <ref> [--fixture <ref>] [--ttl 900] [--environment]
  vlno worlds status <id>
  vlno worlds connect <id> --out <new-private-file>
  vlno mcp --connection <private-file>
  vlno worlds trace <id>
  vlno worlds exec <id> --cwd /home/model --exec-timeout 30 -- <program> [args...]
  vlno worlds task <id>
  vlno worlds evaluate <id>
  vlno worlds reset <id>
  vlno worlds destroy <id>
  vlno worlds operations <id>
  vlno worlds setup <id> --login <login> --email <email> [--alias primary]
  vlno worlds seed <id> <operation> [--args '<json>'] [--alias primary]
  vlno worlds call <id> <operation> [--args '<json>']
  vlno worlds snapshot <id>
  vlno worlds evidence <id>
  vlno version

Global flags (accepted before or after the command):
  --endpoint <origin>       Worker or VLNO product API HTTP(S) origin
  --ca-file <file>          Private CA certificate
  --config <file>           Private configuration file
  --json                   Machine-readable stdout and errors
  --timeout <duration>      Per-request timeout (default 200s)
  --wait-timeout <duration> Lifecycle wait deadline (default 330s)

Connection settings: flags > VLNO_ENDPOINT/VLNO_CA_FILE > saved config.
Credentials: VLNO_API_KEY > saved config. Login reads a key only from stdin.
VLNO_CONFIG selects a config file. Login uses an existing worker API key.
Create/reset: --idempotency-key <key> enables explicit replay.
Create/reset/destroy wait by default; --no-wait returns accepted state.
Seed/call also accept --args-file <file>, or '-' for stdin.
Human output includes progress; JSON output includes lifecycle replay keys.
Catalog includes imported apps and operator-published versioned worlds/scenarios.
Evaluate prints its report; passed exits 0, failed/inconclusive/invalid exit 1.
Generate supports --count 1..4, --distractors 1..4 (both default 2), --no-wait,
and --idempotency-key. Validation uses fresh worlds and can exceed 330s;
increase --wait-timeout or resume inspection with scenarios status <job-id>.
Generation is deterministic built-in workflow variation, not model generation.
Authoring run uses the operator-configured provider; no provider key is submitted
in a job. Role files are exclusive private files; bearer credentials never print.
Authoring submit requires a role credential via the normal credential settings.
Authoring run waits for acceptance; validate waits for the audit stage.
Acceptance includes the required honest-model live gate; live starts that gate
after manually submitted audits have passed.
Apps validate checks structure only; import runs the contract and publishes on success.
source_image must be an immutable sha256 image ID already on the worker.
Import never uploads or pulls an image. Increase --wait-timeout for import validation.
Connect exports scoped HTTP MCP/app connections to a new private file.
Platform commands use a customer API key against VLNO's organization-owned API.
Platform next creates/reuses a private claim file and waits for a ready connection.
Use a NEW claim file per case; reuse it only to recover that same assignment.
Pass only the connection file to the harness, never the customer key or claim file.
Platform record streams UTF-8 stdin to the live trajectory (native JSON is retained).
Use the SDK process recorder to capture stdout and stderr separately.
Platform next and wait use SSE and resume read-only streams after disconnects.
Platform watch streams run status and recorded trajectory; --json emits JSON lines.
Platform finish starts grading; wait exits 0 only for a passed, cleaned-up run.
Create --environment opts into the isolated workstation exec endpoint and MCP tool.
Exec returns a structured result; guest failure/truncation exits 1. Image upload is pending.
`
