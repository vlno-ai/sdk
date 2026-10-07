"""Actual locked/fsynced store composition, without a remote service or model."""
import copy
import json
from pathlib import Path
import sys
import tempfile
import unittest
import uuid
from unittest.mock import patch

from vlno.sessions.files import SessionFileError
from vlno.sessions.store import SessionStore
from session_capture_fixtures import rig


def ready_store(path, command=None):
    fixtures = Path(__file__).resolve().parents[2] / 'testdata/session'
    document = json.loads((fixtures / 'prepared.json').read_text())
    confirmed = json.loads((fixtures / 'confirmed.json').read_text())
    claim = json.loads((fixtures / 'claim.json').read_text())
    if command:
        document['execution']['command'] = command
    store = SessionStore.create(path, document, claim)
    store.update(lambda d: d['admission'].update(state='unknown'))
    store.update(lambda d: d['admission'].update(confirmed['admission']))
    store.update(lambda d: d['claim'].update(state='unknown', actor=d['admission']['actor']))
    store.update(lambda d: d['claim'].update(state='ready', case={
        'index': 0, 'scenario': 'archive-note@1', 'worldId': 'b' * 32,
        'generation': 1, 'expiresAt': '2030-10-07T09:02:00.000Z'}))
    return store


class StoreCaptureTests(unittest.TestCase):
    def test_reopen_after_response_loss_preserves_exact_event_and_records_recovery_gap(self):
        with tempfile.TemporaryDirectory() as root:
            path = str(Path(root) / 'session')
            with ready_store(path) as store:
                recorder, _, _ = rig(store)
                recorder.run._request.side_effect = OSError('response lost')
                with self.assertRaises(OSError):
                    recorder.emit('stdout', {'text': 'ordinary output'})
                original = copy.deepcopy(store.document['capture']['events'][0])
            with SessionStore.open(path) as store:
                recorder, _, sent = rig(store)
                with patch('subprocess.Popen') as spawn, patch('os.killpg') as kill:
                    recorder.recover()
                spawn.assert_not_called()
                kill.assert_not_called()
                self.assertEqual(sent[0]['events'], [original['event']])
                capture = store.document['capture']
                self.assertEqual(capture['events'][0], original)
                self.assertEqual(capture['acknowledgedThrough'], 2)
                self.assertEqual(capture['state'], 'incomplete')
                self.assertTrue(capture['gapRecorded'])

    def test_fsync_failure_poisoned_handle_cannot_upload_or_retry(self):
        with tempfile.TemporaryDirectory() as root:
            with ready_store(str(Path(root) / 'session')) as store:
                recorder, _, sent = rig(store)
                with patch('os.fsync', side_effect=OSError('controlled fsync fault')):
                    with self.assertRaises(OSError):
                        recorder.emit('stdout', {'text': 'must not upload'})
                self.assertEqual(sent, [])
                with self.assertRaises(SessionFileError):
                    recorder.flush()
                self.assertEqual(sent, [])

    def test_supervised_process_completes_only_after_durable_exit_and_exact_acks(self):
        with tempfile.TemporaryDirectory() as root:
            argv = [sys.executable, '-c', 'print("ordinary output")']
            command = {'argv': argv, 'envKeys': [], 'cwd': root}
            with ready_store(str(Path(root) / 'session'), command) as store:
                store.update(lambda d: d['execution'].update(
                    state='start_unknown', launchId=str(uuid.uuid4())))
                recorder, _, sent = rig(store)
                self.assertEqual(recorder.record_process(argv, timeout=5), 0)
                self.assertTrue(sent)
                self.assertEqual(store.document['execution']['state'], 'exited')
                self.assertEqual(store.document['capture']['state'], 'complete')
            with SessionStore.open(str(Path(root) / 'session')) as store:
                self.assertEqual(store.document['capture']['state'], 'complete')
                recorder, _, sent = rig(store)
                with patch('subprocess.Popen') as spawn:
                    recorder.recover()
                spawn.assert_not_called()
                self.assertEqual(sent, [])


if __name__ == '__main__':
    unittest.main()
