"""Useful public diagnostics preserve uncertainty without leaking raw failures."""
from unittest.mock import patch
import time

from vlno import SessionError
from vlno.sessions import protocol
from session_workflow_fixtures import WorkflowCase


class SessionErrorTests(WorkflowCase):
    def test_invalid_input_has_safe_specific_code_and_no_side_effect(self):
        for kwargs, code in [({'timeout': float('nan')}, 'session_invalid_timeout'),
                             ({'env_keys': ['SAME', 'SAME']}, 'session_invalid_command')]:
            with self.subTest(code=code):
                with self.assertRaises(SessionError) as error:
                    self.http.builder(self.path).run_command(self.argv, **kwargs)
                self.assertEqual(error.exception.code, code)
                self.assertFalse(self.path.exists())
        self.assertEqual(self.http.calls, [])

    def test_busy_unsupported_and_incomplete_storage_remain_distinct(self):
        with self.http.builder(self.path) as session:
            session.connect()
            with self.assertRaises(SessionError) as error:
                self.http.client.assessments.open_session(self.path)
            self.assertEqual(error.exception.code, 'session_in_use')
        with patch('vlno.sessions.files.supported_filesystem', return_value=False):
            with self.assertRaises(SessionError) as error:
                self.http.client.assessments.open_session(self.path)
            self.assertEqual(error.exception.code, 'session_filesystem_unsupported')
        (self.path / 'journal.json').unlink()
        with self.assertRaises(SessionError) as error:
            self.http.client.assessments.open_session(self.path)
        self.assertEqual(error.exception.code, 'session_incomplete')
        self.assertTrue((self.path / 'claim.json').exists())

    def test_unknown_network_reply_keeps_lowlevel_code_and_saved_recovery_path(self):
        self.http.drop.add('admit')
        with self.assertRaises(SessionError) as error:
            self.run_command()
        self.assertEqual(error.exception.code, 'transport_error_outcome_unknown')
        self.assertEqual(error.exception.recovery_code, 'admission_unknown')
        self.assertEqual(error.exception.session_path, str(self.path))
        self.assertEqual(error.exception.idempotency_key, self.http.writes('admit')[0][2])

    def test_process_timeout_is_clipped_to_existing_effective_lease(self):
        deadline = time.time() + 2
        self.http.ready['case']['expiresAt'] = deadline
        self.http.ready['case']['connection']['expires_at'] = deadline
        with patch('vlno.sessions.workflow.DurableRecorder.record_process',
                   side_effect=TimeoutError('raw private process diagnostic')) as execute:
            with self.assertRaises(SessionError) as error:
                self.http.builder(self.path).run_command(self.argv, cwd=self.root, timeout=300)
        self.assertLessEqual(execute.call_args.kwargs['timeout'], 2)
        self.assertGreater(execute.call_args.kwargs['timeout'], 0)
        self.assertEqual(error.exception.code, 'session_process_timeout')
        self.assertNotIn('private', str(error.exception))
        self.assertFalse(self.marker.exists())

    def test_authority_delay_crossing_lease_prevents_spawn_at_final_recorder_guard(self):
        clock = [time.time()]
        authorize = protocol.authorize

        def delayed(session, actor=None):
            result = authorize(session, actor)
            if session.store.document['claim']['state'] == 'ready':
                clock[0] = self.http.ready['case']['expiresAt'] + 1
            return result

        with patch('vlno.sessions.capture.time.time', side_effect=lambda: clock[0]):
            with patch.object(protocol, 'authorize', side_effect=delayed):
                with patch('subprocess.Popen') as spawn:
                    with self.assertRaises(SessionError) as error:
                        self.run_command()
                spawn.assert_not_called()
        self.assertEqual(error.exception.code, 'session_connection_expired')
        self.assertEqual(self.http.writes('transcript'), [])
        self.assertEqual(self.http.writes('finish'), [])
        self.assertFalse(self.marker.exists())
