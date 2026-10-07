"""Durable event append/ack primitives; no network or process creation."""
import json
import uuid

from closed_world_sdk import SDKError
from closed_world_sdk.trajectory import KINDS
from .json import encode
from .fields import now

ORDINARY_COUNT, ORDINARY_BYTES = 1000, 768 * 1024
LIFETIME_COUNT, LIFETIME_BYTES = 1024, 1024 * 1024


def event(kind, data, event_id=None, occurred_at=None):
    if kind not in KINDS or type(data) is not dict:
        raise SDKError('session_event_invalid')
    identity = str(uuid.uuid4()) if event_id is None else event_id
    try:
        parsed = uuid.UUID(identity)
        if parsed.version != 4 or str(parsed) != identity:
            raise ValueError()
    except (ValueError, TypeError, AttributeError):
        raise SDKError('session_event_invalid') from None
    return {'id': identity, 'kind': kind, 'occurredAt': now() if occurred_at is None else occurred_at, 'data': data}


def append(store, value, *, reserved=False):
    size = len(encode(value))
    if len(json.dumps({'claim': 'A' * 43, 'events': [value]}).encode()) > 65536:
        raise SDKError('session_event_too_large')
    if size > (1024 if reserved else 24000):
        raise SDKError('session_event_too_large')
    def change(document):
        capture = document['capture']
        rows = capture['events']
        total = sum(len(encode(row['event'])) for row in rows) + size
        count_limit, byte_limit = ((LIFETIME_COUNT, LIFETIME_BYTES) if reserved
                                   else (ORDINARY_COUNT, ORDINARY_BYTES))
        if len(rows) >= count_limit or total > byte_limit:
            raise SDKError('outbox_full')
        if any(row['event']['id'] == value['id'] for row in rows):
            raise SDKError('session_event_duplicate')
        rows.append({'sequence': capture['nextSequence'], 'event': value})
        capture['nextSequence'] += 1
        if capture['state'] == 'not_started':
            capture['state'] = 'open'
    store.update(change)


def gap(store, code='capture_incomplete'):
    """Best effort caller may catch storage errors; no upload after failed write."""
    if store.document['capture']['gapRecorded']:
        return next(row['event']['id'] for row in store.document['capture']['events'] if row['event']['kind'] == 'gap')
    value = event('gap', {'event': 'capture_gap', 'reason': code})
    def change(document):
        capture = document['capture']
        rows = capture['events']
        if len(rows) >= LIFETIME_COUNT or sum(len(encode(r['event'])) for r in rows) + len(encode(value)) > LIFETIME_BYTES:
            raise SDKError('outbox_full')
        rows.append({'sequence': capture['nextSequence'], 'event': value})
        capture.update(nextSequence=capture['nextSequence'] + 1, gapRecorded=True, state='incomplete')
        document['recovery'] = {'code': code, 'occurredAt': now()}
    store.update(change)
    return value['id']


def acknowledge(store, sequence, identity):
    def change(document):
        capture = document['capture']
        if capture['acknowledgedThrough'] + 1 != sequence or capture['events'][sequence - 1]['event']['id'] != identity:
            raise SDKError('invalid_transcript_acknowledgement')
        capture['acknowledgedThrough'] = sequence
    store.update(change)


def complete(store):
    def change(document):
        capture, execution = document['capture'], document['execution']
        rows = capture['events']
        expected = {'event': 'capture_finished', 'exitCode': execution['exitCode'], 'streams': ['stdout', 'stderr']}
        if (execution['mode'] != 'supervised' or execution['state'] != 'exited'
                or capture['gapRecorded'] or not rows
                or capture['acknowledgedThrough'] != len(rows)
                or rows[-1]['event']['kind'] != 'lifecycle'
                or rows[-1]['event']['data'] != expected):
            return
        capture['state'] = 'complete'
    store.update(change)
