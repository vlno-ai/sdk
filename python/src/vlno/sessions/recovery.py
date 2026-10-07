"""Recovery of saved protocol intent, never application or process replay."""
from closed_world_sdk import SDKError
from . import protocol
from .fields import now
from .outbox import gap


def resume(session):
    document = session.store.document
    if document['admission']['state'] == 'prepared':
        raise SDKError('session_admission_unconfirmed')
    actor = (document['admission']['actor'] if document['admission']['state'] != 'confirmed'
             else document['cancellation']['actor'] if document['cancellation']['state'] == 'unknown'
             else document['claim']['actor'])
    protocol.authorize(session, actor)
    if document['execution']['state'] in {'start_unknown', 'running'}:
        session.store.update(lambda d: (
            d['execution'].update(state='exit_unknown'),
            d.update(recovery={'code': 'exit_unknown', 'occurredAt': now()})))
        gap(session.store)
    elif document['capture']['state'] == 'open':
        gap(session.store)
    if document['admission']['state'] == 'unknown':
        protocol.admit(session)
    document = session.store.document
    # A saved cancellation must not trigger fresh allocation while recovering.
    if document['cancellation']['state'] == 'unknown':
        protocol.cancel(session)
        return protocol.observe(session)
    if document['claim']['state'] == 'unknown':
        protocol.claim(session)
    if session.store.document['claim']['case'] is not None:
        protocol.flush(session)
    if session.store.document['finish']['state'] == 'unknown':
        protocol.finish(session)
    return protocol.observe(session)
