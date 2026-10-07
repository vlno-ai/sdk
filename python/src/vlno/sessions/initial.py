"""Build one immutable admission intent only after current readiness/identity."""
import copy
import hashlib
import secrets
import uuid

from closed_world_sdk import SDKError
from .fields import command as validate_command, now
from .identity import identity
from .store import SessionStore
from .files import SessionFileError


def create(assessments, path, assessment_id, revision_id, mode, command):
    if command is not None:
        try:
            validate_command(command)
        except SessionFileError:
            raise SDKError('session_invalid_command') from None
        secret = assessments._client._transport.api_key
        if any(secret in arg for arg in command['argv']):
            raise SDKError('session_command_contains_credentials')
    prepared = assessments.prepare(assessment_id, revision_id)
    current = identity(assessments._client)
    if prepared['orgId'] != current['orgId'] or prepared['actor'] != current['actor']:
        raise SDKError('session_identity_changed')
    if prepared['state'] != 'ready' or not prepared['admissionAccess']['canAdmit']:
        raise SDKError('session_scope_not_ready')
    source = copy.deepcopy(prepared['source'])
    claim, stamp, ident = secrets.token_urlsafe(32), now(), str(uuid.uuid4())
    document = {
        'schema': 'vlno.client-session/1', 'id': ident, 'revision': 1,
        'createdAt': stamp, 'updatedAt': stamp, 'endpoint': current['endpoint'],
        'orgId': current['orgId'], 'source': source,
        'admission': {'state': 'prepared', 'actor': current['actor'],
            'idempotencyKey': str(uuid.uuid4()), 'command': {k: source[k] for k in
                ('planRevisionId', 'approvalReviewId', 'systemRevisionId')}, 'receipt': None},
        'claim': {'file': 'claim.json', 'sha256': hashlib.sha256(claim.encode()).hexdigest(),
                  'state': 'not_started', 'actor': None, 'case': None},
        'execution': {'mode': mode, 'state': 'not_started', 'launchId': None,
                      'command': copy.deepcopy(command), 'pid': None, 'exitCode': None, 'finishedAt': None},
        'capture': {'state': 'not_started', 'nextSequence': 1, 'acknowledgedThrough': 0,
                    'gapRecorded': False, 'events': []},
        'finish': {'state': 'not_started', 'actor': None, 'command': None},
        'cancellation': {'state': 'not_requested', 'actor': None},
        'observation': None, 'recovery': None,
    }
    return SessionStore.create(path, document,
        {'schema': 'vlno.client-session-claim/1', 'sessionId': ident, 'claim': claim})
