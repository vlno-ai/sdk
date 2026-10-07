"""Synthetic approval-bound fixtures matching the public A02 contract."""

from test_platform import RUN, view, assigned

ASSESSMENT = "11111111-1111-4111-8111-111111111111"
REVISION = "22222222-2222-4222-8222-222222222222"
APPROVAL = "33333333-3333-4333-8333-333333333333"
SYSTEM = "44444444-4444-4444-8444-444444444444"
VERSION = "55555555-5555-4555-8555-555555555555"
TEMPLATE = "66666666-6666-4666-8666-666666666666"
REPLAY = "saved-admission-key"
SOURCE = {
    "assessmentId": ASSESSMENT,
    "planRevisionId": REVISION,
    "approvalReviewId": APPROVAL,
    "systemId": SYSTEM,
    "systemRevisionId": VERSION,
    "templateId": TEMPLATE,
}
ACTOR = {"kind": "service_key", "reference": "key-actor-opaque"}
COMMAND = {
    "planRevisionId": REVISION,
    "approvalReviewId": APPROVAL,
    "systemRevisionId": VERSION,
}


def fixture(name):
    import json
    from pathlib import Path

    path = Path(__file__).resolve().parents[2] / "testdata" / f"assessment-{name}.json"
    return json.loads(path.read_text())


def readiness(can_admit=True):
    value = fixture("preparation")
    if not can_admit:
        value["admissionAccess"] = {"canAdmit": False, "reason": "grant_missing"}
    return value


def admitted():
    return fixture("admission")


def status():
    return fixture("status")


def lifecycle():
    value = view()
    value["result"]["cases"][0]["scenario"] = "archive-note@1"
    return value


def connection():
    value = assigned()
    value["run"] = lifecycle()
    value["case"]["scenario"] = "archive-note@1"
    return value
