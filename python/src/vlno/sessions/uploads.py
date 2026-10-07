"""Replay already scrubbed events without recovering a scoped app connection."""
from closed_world_sdk import SDKError
from .outbox import acknowledge


def flush_saved(store, run, case_index):
    while True:
        capture = store.document['capture']
        sequence = capture['acknowledgedThrough'] + 1
        if sequence == capture['nextSequence']:
            return
        value = capture['events'][sequence - 1]['event']
        reply = run._request('POST', f'/cases/{case_index}/transcript',
                             {'claim': store.claim, 'events': [value]})
        if type(reply) is not dict or reply.get('accepted') != [value['id']]:
            raise SDKError('invalid_transcript_acknowledgement', run_id=run.id)
        acknowledge(store, sequence, value['id'])
