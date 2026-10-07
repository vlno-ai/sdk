"""Consume explicitly approved scopes without creating or impersonating reviewers."""

from closed_world_sdk import SDKError

from .assessment_fields import UUID, KEY, RUN, identifier
from .assessment_preparation import admission, preparation
from .approved_run import ApprovedRun


class Assessments:
    def __init__(self, client):
        self._client = client

    def session(self, path, *, assessment_id, plan_revision_id):
        """Plan local durable work; no directory or run exists until connect/run_command."""
        from .sessions.workflow import Session
        identifier(UUID, assessment_id, "assessment ID")
        identifier(UUID, plan_revision_id, "scope revision ID")
        return Session(self, path, assessment_id=assessment_id, plan_revision_id=plan_revision_id)

    def open_session(self, path):
        """Lock and validate existing intent. Opening never sends or executes work."""
        from .sessions.workflow import Session
        return Session.open(self, path)

    def prepare(self, assessment_id, plan_revision_id):
        identifier(UUID, assessment_id, "assessment ID")
        identifier(UUID, plan_revision_id, "scope revision ID")
        path = f"/v1/assessments/{assessment_id}/revisions/{plan_revision_id}/run-preparation"
        return preparation(
            self._client._transport.request("GET", path),
            assessment_id,
            plan_revision_id,
        )

    def admit(
        self,
        assessment_id,
        *,
        plan_revision_id,
        approval_review_id,
        system_revision_id,
        idempotency_key,
    ):
        """Reserve one run with a caller-saved replay key. Does not start the agent.

        On response loss, retry this exact call with the same saved identifiers/key.
        The immutable receipt identifies the original run even if its state changes.
        """
        identifier(UUID, assessment_id, "assessment ID")
        command = {
            "planRevisionId": identifier(UUID, plan_revision_id, "scope revision ID"),
            "approvalReviewId": identifier(UUID, approval_review_id, "approval ID"),
            "systemRevisionId": identifier(
                UUID, system_revision_id, "system revision ID"
            ),
        }
        identifier(KEY, idempotency_key, "idempotency key")
        try:
            value = self._client._transport.request(
                "POST",
                f"/v1/assessments/{assessment_id}/runs",
                command,
                key=idempotency_key,
            )
        except SDKError as error:
            error.idempotency_key = idempotency_key
            if error.code in {"response_too_large", "invalid_response"}:
                raise SDKError(
                    "admission_outcome_unknown", idempotency_key=idempotency_key
                ) from error
            raise
        try:
            return admission(value, assessment_id, command, idempotency_key)
        except SDKError as error:
            raise SDKError(
                "admission_outcome_unknown", idempotency_key=idempotency_key
            ) from error

    def get_run(self, run_id):
        """Attach to an existing run; no new run, case, or agent invocation."""
        identifier(RUN, run_id, "product run ID")
        existing = self._client.runs.get(run_id)
        run = ApprovedRun(self._client, existing.info)
        run.assessment()  # Require an actual approval-bound run, not an unbound legacy run.
        return run
