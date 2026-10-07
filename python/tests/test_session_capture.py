"""Durability/ack/redaction properties without any application or model calls."""
import copy
import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from closed_world_sdk import SDKError
from vlno.sessions.capture import DurableRecorder
from vlno.sessions.files import PrivateDirectory
from session_capture_fixtures import CLAIM, KEY, TOKEN, SinkStore, rig


class DurableCaptureTests(unittest.TestCase):
    def test_response_loss_and_ack_loss_replay_identical_oldest_event(self):
        recorder, store, sent = rig()
        original = recorder.run._request.side_effect
        def lost(*args):
            original(*args)
            raise SDKError('response_lost')
        recorder.run._request.side_effect = lost
        with self.assertRaises(SDKError):
            recorder.emit('stdout', {'text': 'hello'})
        event = store.document['capture']['events'][0]['event']
        self.assertEqual(store.document['capture']['acknowledgedThrough'], 0)
        recorder.run._request.side_effect = original
        store.fail_at = store.calls + 1
        with self.assertRaises(OSError):
            recorder.flush()
        store.fail_at = None
        reopened = DurableRecorder(recorder.run, recorder.case, CLAIM, store)
        reopened.flush()
        self.assertEqual([body['events'] for body in sent], [[event]] * 3)
        self.assertEqual(store.document['capture']['acknowledgedThrough'], 1)
        self.assertEqual(store.document['capture']['state'], 'open')

    def test_wrong_ack_never_advances_prefix(self):
        recorder, store, _ = rig()
        recorder.run._request.return_value = {'accepted': ['different']}
        recorder.run._request.side_effect = None
        with self.assertRaises(SDKError):
            recorder.emit('stdout', {'text': 'hello'})
        self.assertEqual(store.document['capture']['acknowledgedThrough'], 0)
        self.assertEqual(len(store.document['capture']['events']), 1)

    def test_real_file_fsync_failure_prevents_upload(self):
        with tempfile.TemporaryDirectory() as directory:
            with PrivateDirectory(Path(directory) / 'session', create=True) as files:
                recorder, store, _ = rig(SinkStore(files))
                before = files.read('journal.json', 2 * 1024 * 1024)
                with patch('vlno.sessions.files.os.fsync', side_effect=OSError('failure')):
                    with self.assertRaises(OSError):
                        recorder.emit('stdout', {'text': 'must not upload'})
                recorder.run._request.assert_not_called()
                self.assertEqual(files.read('journal.json', 2 * 1024 * 1024), before)
                self.assertTrue(store.failed)

    def test_redaction_covers_keys_values_and_rejects_collisions(self):
        recorder, store, _ = rig()
        recorder.emit('tool_result', {KEY: {'value': TOKEN, 'nested': [{'claim': CLAIM}]}})
        text = json.dumps(store.document['capture'])
        for secret in (KEY, TOKEN, CLAIM):
            self.assertNotIn(secret, text)
        original = copy.deepcopy(store.document['capture']['events'])
        with self.assertRaisesRegex(SDKError, 'session_redaction_collision'):
            recorder.emit('stdout', {KEY: 'one', TOKEN: 'two'})
        self.assertEqual(store.document['capture']['events'][:-1], original)
        self.assertTrue(store.document['capture']['gapRecorded'])
        self.assertEqual(recorder.run._request.call_count, 1)

    def test_recovery_records_one_gap_even_after_every_saved_event_was_acknowledged(self):
        recorder, store, _ = rig()
        recorder.emit('stdout', {'text': 'some output'})
        with patch('subprocess.Popen') as spawn:
            reopened = DurableRecorder(recorder.run, recorder.case, CLAIM, store)
            reopened.recover()
            rows = store.document['capture']['events']
            reopened.recover()
        spawn.assert_not_called()
        self.assertEqual(store.document['capture']['events'], rows)
        self.assertEqual(store.document['capture']['state'], 'incomplete')
        self.assertTrue(store.document['capture']['gapRecorded'])

    def test_native_unicode_event_must_fit_actual_python_transport_envelope(self):
        recorder, store, _ = rig()
        with self.assertRaisesRegex(SDKError, 'session_event_too_large'):
            recorder.emit('stdout', {'text': '🌏' * 5900})
        recorder.run._request.assert_not_called()
        self.assertEqual([r['event']['kind'] for r in store.document['capture']['events']], ['gap'])

    def test_foreign_journal_binding_cannot_upload_or_spawn(self):
        recorder, store, _ = rig()
        store._document['admission']['receipt']['runId'] = 'cw_' + 'f' * 32
        with patch('subprocess.Popen') as spawn:
            with self.assertRaisesRegex(SDKError, 'session_capture_binding_changed'):
                DurableRecorder(recorder.run, recorder.case, CLAIM, store)
        spawn.assert_not_called()
        recorder.run._request.assert_not_called()

    def test_new_ordinary_capture_stops_after_gap(self):
        recorder, store, _ = rig()
        recorder.emit('gap', {})
        before = store.document['capture']['events']
        with self.assertRaisesRegex(SDKError, 'session_capture_incomplete'):
            recorder.emit('stdout', {'text': 'not silently resumed'})
        self.assertEqual(store.document['capture']['events'], before)

    def test_changed_platform_case_pins_reject_before_durability_upload_or_launch(self):
        for field, value in [('world_id', 'c' * 32), ('scenario', 'other@1'),
                             ('expires_at', 1), ('generation', 2), ('generation', True)]:
            with self.subTest(field=field, value=value):
                recorder, store, _ = rig()
                before = store.document
                if field == 'generation':
                    recorder.case.agent._manifest[field] = value
                else:
                    setattr(recorder.case, field, value)
                with patch('subprocess.Popen') as spawn:
                    with self.assertRaisesRegex(SDKError, 'session_capture_binding_changed'):
                        DurableRecorder(recorder.run, recorder.case, CLAIM, store)
                spawn.assert_not_called()
                recorder.run._request.assert_not_called()
                self.assertEqual(store.document, before)

    def test_effective_expiry_floors_submillisecond_without_extending_saved_lease(self):
        recorder, store, _ = rig()
        recorder.case.expires_at += .0005
        DurableRecorder(recorder.run, recorder.case, CLAIM, store)
        recorder.case.expires_at += .001
        with self.assertRaisesRegex(SDKError, 'session_capture_binding_changed'):
            DurableRecorder(recorder.run, recorder.case, CLAIM, store)


if __name__ == '__main__':
    unittest.main()
