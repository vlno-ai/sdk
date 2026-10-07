"""Durable trajectory sink. Reopening/flushing never launches a process."""
from datetime import datetime
import math
import time
from closed_world_sdk import SDKError
from closed_world_sdk.trajectory import TrajectoryRecorder
from . import outbox
from .uploads import flush_saved


def matches_case(saved, case):
    try:
        generation = case.agent.connection()['generation']
        return (saved is not None and saved['index'] == case.index
                and saved['worldId'] == case.world_id and saved['scenario'] == case.scenario
                and type(generation) is int and saved['generation'] == generation
                and type(case.expires_at) in (int, float) and math.isfinite(case.expires_at)
                and round(datetime.fromisoformat(saved['expiresAt']).timestamp() * 1000)
                == math.floor(case.expires_at * 1000))
    except (AttributeError, KeyError, TypeError, ValueError, OverflowError):
        return False


class DurableRecorder(TrajectoryRecorder):
    def __init__(self, run, case, claim, store):
        super().__init__(run, case, claim)
        if claim != store.claim:
            raise SDKError('session_claim_mismatch')
        document = store.document
        saved_case = document['claim']['case']
        if (document['admission']['state'] != 'confirmed'
                or document['admission']['receipt']['runId'] != run.id
                or document['claim']['state'] not in {'ready', 'ended'}
                or not matches_case(saved_case, case)):
            raise SDKError('session_capture_binding_changed')
        self.store = store
        self._observed_exit = False
        self._capturing = False

    def _clean(self, value):
        if isinstance(value, dict):
            result = {}
            for key, item in value.items():
                if not isinstance(key, str):
                    raise SDKError('session_event_invalid')
                cleaned = super()._clean({key: item})
                name = next(iter(cleaned))
                if name in result:
                    raise SDKError('session_redaction_collision')
                result.update(cleaned)
            return result
        return super()._clean(value)

    def _gap(self, code='capture_incomplete'):
        try:
            outbox.gap(self.store, code)
        except Exception:
            pass  # Preserve the original error; ambiguous storage must be reopened.

    def emit(self, kind, data, *, event_id=None, occurred_at=None):
        with self._lock:
            if self.store.document['capture']['state'] == 'complete':
                raise SDKError('session_capture_complete')
            if self.store.document['capture']['gapRecorded'] and kind != 'gap':
                raise SDKError('session_capture_incomplete')
            if kind == 'gap':
                identity = outbox.gap(self.store)
                # Process error cleanup must not retry an uncertain upload.
                # Preserve the gap locally for an explicit recovery operation.
                if not self._capturing:
                    self.flush()
                return identity
            try:
                value = outbox.event(kind, self._clean(data), event_id, occurred_at)
                reserved = self._observed_exit and kind == 'lifecycle' and data.get('event') == 'capture_finished'
                outbox.append(self.store, value, reserved=reserved)
            except Exception as error:
                self._gap('outbox_full' if getattr(error, 'code', None) == 'outbox_full' else 'capture_incomplete')
                raise
            self.flush()
            if reserved:
                outbox.complete(self.store)
            return value['id']

    def flush(self):
        with self._lock:
            flush_saved(self.store, self.run, self.case.index)

    def recover(self):
        """An interrupted open capture cannot be repaired by an empty outbox."""
        with self._lock:
            if self.store.document['capture']['state'] == 'open':
                outbox.gap(self.store)
            self.flush()

    def record_process(self, argv, *, input=None, env_keys=(), timeout=300, cwd=None):
        with self._lock:
            document = self.store.document
            execution, capture = document['execution'], document['capture']
            command = execution['command']
            if (execution['mode'] != 'supervised' or execution['state'] != 'start_unknown'
                    or capture['state'] != 'not_started' or command is None or self._capturing):
                raise SDKError('session_launch_not_permitted')
            cwd = command['cwd'] if cwd is None else cwd
            if (argv != command['argv'] or list(env_keys) != command['envKeys'] or cwd != command['cwd']
                    or any(secret in arg for secret in self._secrets for arg in argv)):
                raise SDKError('session_command_changed')
            self._capturing = True
            try:
                timeout = min(timeout, self.case.expires_at - time.time())
                if timeout <= 0:
                    raise SDKError('session_connection_expired')
                return super().record_process(argv, input=input, env_keys=env_keys, timeout=timeout, cwd=cwd)
            except BaseException:
                self._gap()
                raise
            finally:
                self._capturing = False

    def _process_started(self, process):
        def change(document):
            document['execution'].update(state='running', pid=process.pid)
        self.store.update(change)

    def _process_exited(self, code):
        def change(document):
            document['execution'].update(state='exited', exitCode=code, finishedAt=outbox.now())
        self.store.update(change)
        self._observed_exit = True
