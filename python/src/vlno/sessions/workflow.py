"""One durable customer run. Resume never starts a process or app operation."""
from pathlib import Path
import time
import uuid

from closed_world_sdk import SDKError
from . import initial, protocol, recovery
from .capture import DurableRecorder
from .errors import failure, timeout_value
from .store import SessionStore


class Session:
    def __init__(self, assessments, path, *, assessment_id=None, plan_revision_id=None, store=None):
        self.assessments, self.client = assessments, assessments._client
        self.path = str(Path(path).absolute())
        self.assessment_id, self.revision_id = assessment_id, plan_revision_id
        self.store, self._closed = store, False

    @classmethod
    def open(cls, assessments, path):
        session = cls(assessments, path)
        session._call(lambda: setattr(session, 'store', SessionStore.open(path)))
        return session

    def _call(self, action):
        try:
            if self._closed:
                raise SDKError('session_closed')
            return action()
        except Exception as error:
            raise failure(self, error) from None

    def _create(self, mode, command=None):
        if self.store is not None:
            raise SDKError('session_launch_not_permitted')
        self.store = initial.create(self.assessments, self.path, self.assessment_id,
                                    self.revision_id, mode, command)

    def connect(self, *, timeout=330):
        """Explicit external-harness connection; never executes or finishes it."""
        def action():
            timeout_value(timeout)
            if self.store is None:
                self._create('external')
            elif self.store.document['execution']['mode'] != 'external':
                raise SDKError('session_launch_not_permitted')
            protocol.admit(self)
            case = protocol.claim(self, new=True, timeout=timeout)
            if case is not None and self.store.document['execution']['state'] == 'not_started':
                self.store.update(lambda d: d['execution'].update(state='external_active'))
            return case
        return self._call(action)

    def run_command(self, argv, *, env_keys=(), cwd=None, timeout=300):
        """Launch this exact command once; close the journal on return or error."""
        def action():
            timeout_value(timeout)
            command = {'argv': argv, 'envKeys': list(env_keys), 'cwd': str(Path(cwd or Path.cwd()).absolute())}
            self._create('supervised', command)
            protocol.admit(self)
            case = protocol.claim(self, new=True, timeout=timeout)
            if case is None:
                return protocol.observe(self)
            remaining = case.expires_at - time.time()
            if remaining <= 0:
                raise SDKError('session_connection_expired')
            protocol.authorize(self, self.store.document['claim']['actor'])
            self.store.update(lambda d: d['execution'].update(
                state='start_unknown', launchId=str(uuid.uuid4())))
            recorder = DurableRecorder(protocol.run(self), case, self.store.claim, self.store)
            code = recorder.record_process(argv, env_keys=env_keys, cwd=command['cwd'],
                                           timeout=min(timeout, remaining))
            protocol.finish(self, 'completed' if code == 0 else 'error')
            protocol.run(self).wait()
            return protocol.observe(self)
        try:
            return self._call(action)
        finally:
            self.close()

    def resume(self):
        """Settle saved protocol requests only; never launch or call app tools."""
        return self._call(lambda: recovery.resume(self))

    def finish(self, *, agent_status):
        """Explicit declaration; it is not a grade or proof of complete capture."""
        def action():
            protocol.finish(self, agent_status)
            return protocol.observe(self)
        return self._call(action)

    def cancel(self):
        def action():
            protocol.cancel(self)
            return protocol.observe(self)
        return self._call(action)

    def status(self):
        return self._call(lambda: protocol.observe(self))

    def close(self):
        if self.store is not None:
            self.store.close()
        self._closed = True

    def __enter__(self):
        if self._closed:
            raise SDKError('session_closed')
        return self

    def __exit__(self, *_):
        self.close()
