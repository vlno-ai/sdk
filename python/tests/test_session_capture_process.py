"""Only explicit new supervision can launch; resume never interprets a saved PID."""
import copy
import os
from pathlib import Path
import sys
import tempfile
import unittest
import uuid
from unittest.mock import patch

from closed_world_sdk import SDKError
from vlno.sessions.capture import DurableRecorder
from vlno.sessions import outbox
from session_capture_fixtures import CLAIM, rig


class DurableProcessTests(unittest.TestCase):
    def command(self, store, argv, cwd):
        store._document['execution'].update(state='start_unknown', launchId=str(uuid.uuid4()),
            command={'argv': argv, 'envKeys': [], 'cwd': cwd})

    def test_observed_stream_ends_exit_and_ack_complete_capture_with_explicit_cwd(self):
        recorder, store, sent = rig()
        with tempfile.TemporaryDirectory() as directory:
            argv = [sys.executable, '-c', 'import os,sys; print(os.getcwd()); print("diagnostic",file=sys.stderr)']
            self.command(store, argv, directory)
            before = os.getcwd()
            self.assertEqual(recorder.record_process(argv, timeout=5), 0)
            self.assertEqual(os.getcwd(), before)
            self.assertIn(directory, ''.join(e['events'][0]['data'].get('text', '') for e in sent))
        execution, capture = store.document['execution'], store.document['capture']
        self.assertEqual(execution['state'], 'exited')
        self.assertEqual(execution['exitCode'], 0)
        self.assertEqual(capture['state'], 'complete')
        self.assertEqual(capture['acknowledgedThrough'], len(capture['events']))
        self.assertFalse(capture['gapRecorded'])
        with patch('subprocess.Popen') as spawn:
            with self.assertRaisesRegex(SDKError, 'session_launch_not_permitted'):
                recorder.record_process(argv)
        spawn.assert_not_called()

    def test_started_journal_failure_stops_only_owned_child(self):
        recorder, store, _ = rig()
        argv = [sys.executable, '-c', 'import time; time.sleep(30)']
        self.command(store, argv, str(Path.cwd()))
        real = recorder._process_started
        owned = []
        def fail(process):
            owned.append(process)
            store.fail_at = store.calls + 1
            real(process)
        with patch.object(recorder, '_process_started', side_effect=fail):
            with self.assertRaises(OSError):
                recorder.record_process(argv, timeout=5)
        self.assertEqual(len(owned), 1)
        self.assertIsNotNone(owned[0].poll())
        self.assertEqual(store.document['capture']['state'], 'incomplete')

    def test_spoofed_completion_event_and_empty_upload_queue_do_not_prove_complete(self):
        recorder, store, _ = rig()
        store._document['execution'].update(mode='external', state='external_active', command=None)
        recorder.emit('lifecycle', {'event': 'capture_finished', 'exitCode': 0, 'streams': ['stdout', 'stderr']})
        self.assertEqual(store.document['capture']['state'], 'open')
        self.assertEqual(store.document['capture']['acknowledgedThrough'], 1)

    def test_ordinary_limit_preserves_lifetime_history_and_reserves_one_gap(self):
        recorder, store, _ = rig()
        with patch.object(outbox, 'ORDINARY_COUNT', 2):
            recorder.emit('stdout', {'text': 'one'})
            recorder.emit('stderr', {'text': 'two'})
            before = copy.deepcopy(store.document['capture']['events'])
            with self.assertRaisesRegex(SDKError, 'outbox_full'):
                recorder.emit('stdout', {'text': 'three'})
        capture = store.document['capture']
        self.assertEqual(capture['events'][:-1], before)
        self.assertEqual(capture['events'][-1]['event']['kind'], 'gap')
        recorder.recover()
        self.assertEqual(len(store.document['capture']['events']), 3)
        self.assertEqual(store.document['capture']['state'], 'incomplete')

    def test_full_lifetime_acknowledged_history_counts_toward_capture_limit(self):
        recorder, store, _ = rig()
        rows = [{'sequence': i + 1, 'event': outbox.event('stdout', {'text': 'saved'})}
                for i in range(1000)]
        store._document['capture'].update(state='open', events=rows,
                                         nextSequence=1001, acknowledgedThrough=1000)
        with self.assertRaisesRegex(SDKError, 'outbox_full'):
            recorder.emit('stdout', {'text': 'overflow'})
        capture = store.document['capture']
        self.assertEqual(capture['events'][:-1], rows)
        self.assertEqual(capture['nextSequence'], 1002)
        self.assertEqual(capture['acknowledgedThrough'], 1000)
        self.assertEqual(capture['events'][-1]['event']['kind'], 'gap')

    def test_lifetime_byte_limit_reserves_gap_without_rewriting_prior_events(self):
        recorder, store, _ = rig()
        rows = [{'sequence': i + 1, 'event': outbox.event('stdout', {'text': 'x' * 20000})}
                for i in range(39)]
        store._document['capture'].update(state='open', events=rows,
                                         nextSequence=40, acknowledgedThrough=39)
        with self.assertRaisesRegex(SDKError, 'outbox_full'):
            recorder.emit('stdout', {'text': 'x' * 20000})
        self.assertEqual(store.document['capture']['events'][:-1], rows)
        self.assertEqual(store.document['capture']['events'][-1]['event']['kind'], 'gap')

    def test_recovery_does_not_signal_saved_pid_or_spawn(self):
        recorder, store, _ = rig()
        self.command(store, ['example-harness'], '/tmp')
        store._document['execution'].update(state='running', pid=os.getpid())
        store._document['capture']['state'] = 'open'
        with patch('subprocess.Popen') as spawn, patch('os.killpg') as kill:
            DurableRecorder(recorder.run, recorder.case, CLAIM, store).recover()
        spawn.assert_not_called()
        kill.assert_not_called()
        self.assertEqual(store.document['capture']['state'], 'incomplete')


if __name__ == '__main__':
    unittest.main()
