"""Durable protocol intents. No application tools or process creation here."""
from datetime import datetime, timezone
import math

from closed_world_sdk import SDKError
from .identity import identity, endpoint_origin
from .fields import now
from .uploads import flush_saved
from .outbox import gap


def authorize(session, actor=None):
    document = session.store.document
    if endpoint_origin(session.client._transport.endpoint) != document['endpoint']:
        raise SDKError('session_identity_changed')
    current = identity(session.client)
    if (current['orgId'] != document['orgId']
            or actor is not None and current['actor'] != actor):
        raise SDKError('session_identity_changed')
    return current['actor']


def admit(session):
    saved = session.store.document['admission']
    if saved['state'] == 'confirmed':
        return
    authorize(session, saved['actor'])
    session.store.update(lambda d: d['admission'].update(state='unknown'))
    command = saved['command']
    receipt = session.assessments.admit(session.store.document['source']['assessmentId'],
        plan_revision_id=command['planRevisionId'], approval_review_id=command['approvalReviewId'],
        system_revision_id=command['systemRevisionId'], idempotency_key=saved['idempotencyKey'])
    session.store.update(lambda d: d['admission'].update(state='confirmed', receipt=receipt))


def run(session):
    document = session.store.document
    receipt = document['admission']['receipt']
    if receipt is None:
        raise SDKError('session_admission_unconfirmed')
    attached = session.assessments.get_run(receipt['runId'])
    current = attached.assessment()
    if (current['source'] != document['source'] or current['orgId'] != document['orgId']
            or current['admittedBy'] != document['admission']['actor']):
        raise SDKError('session_source_changed')
    return attached


def observe(session):
    authorize(session)
    value = run(session).assessment()
    session.store.update(lambda d: d.update(observation={'checkedAt': now(), 'value': value}))
    return value


def claim(session, *, new=False, timeout=330):
    saved = session.store.document['claim']
    if saved['state'] == 'ended':
        return None
    if saved['state'] == 'not_started' and not new:
        return None
    actor = authorize(session, saved['actor'])
    if saved['state'] == 'not_started':
        session.store.update(lambda d: d['claim'].update(state='unknown', actor=actor))
    assigned = run(session).next(session.store.claim, timeout=timeout)
    if assigned is None:
        session.store.update(lambda d: d['claim'].update(state='ended'))
        return None
    millis = math.floor(assigned.expires_at * 1000)
    value = {'index': assigned.index, 'scenario': assigned.scenario, 'worldId': assigned.world_id,
             'generation': assigned.agent.connection()['generation'],
             'expiresAt': datetime.fromtimestamp(millis / 1000, timezone.utc)
                          .isoformat(timespec='milliseconds').replace('+00:00', 'Z')}
    session.store.update(lambda d: d['claim'].update(state='ready', case=value))
    return assigned


def flush(session):
    saved = session.store.document['claim']
    if saved['case'] is None:
        return
    authorize(session, saved['actor'])
    flush_saved(session.store, run(session), saved['case']['index'])


def finish(session, agent_status=None, timeout=330):
    document = session.store.document
    saved = document['finish']
    if saved['state'] != 'not_started' and agent_status is not None and agent_status != saved['command']['agent_status']:
        raise SDKError('session_finish_changed')
    if saved['state'] == 'accepted':
        return
    if saved['state'] == 'not_started':
        if agent_status not in {'completed', 'error', 'timeout'} or document['claim']['case'] is None:
            raise SDKError('session_finish_not_permitted')
        if agent_status == 'completed' and document['execution']['mode'] == 'supervised' and (document['capture']['state'] != 'complete'
                or document['execution']['state'] != 'exited'):
            raise SDKError('session_capture_incomplete')
        actor = authorize(session, document['claim']['actor'])
        if document['execution']['mode'] == 'external':
            gap(session.store)
        flush(session)
        command = {'case_index': document['claim']['case']['index'], 'agent_status': agent_status}
        session.store.update(lambda d: d['finish'].update(state='unknown', actor=actor, command=command))
        saved = session.store.document['finish']
    elif agent_status is not None and agent_status != saved['command']['agent_status']:
        raise SDKError('session_finish_changed')
    authorize(session, saved['actor'])
    flush(session)
    run(session).finish(saved['command']['case_index'], session.store.claim,
                        agent_status=saved['command']['agent_status'], timeout=timeout)
    session.store.update(lambda d: d['finish'].update(state='accepted'))


def cancel(session):
    document = session.store.document
    if document['admission']['receipt'] is None:
        raise SDKError('session_admission_unconfirmed')
    saved = document['cancellation']
    actor = authorize(session, saved['actor'])
    if saved['state'] == 'not_requested':
        session.store.update(lambda d: d['cancellation'].update(state='unknown', actor=actor))
    reply = run(session).cancel()
    if reply.get('cancellationRequested') is not True and reply['state'] not in {'completed', 'failed', 'cancelled'}:
        raise SDKError('session_cancel_outcome_unknown')
    session.store.update(lambda d: d['cancellation'].update(state='requested'))
