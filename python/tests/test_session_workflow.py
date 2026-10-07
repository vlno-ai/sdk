"""Actual HTTP with dropped committed replies; no model or application calls."""
import unittest
from unittest.mock import patch

from vlno.sessions.errors import SessionError
from session_workflow_fixtures import WorkflowCase


class WorkflowTests(WorkflowCase):
    def test_builder_defers_storage_and_complete_command_runs_exactly_once(self):
        builder = self.http.builder(self.path)
        self.assertFalse(self.path.exists())
        self.assertEqual(self.http.calls, [])
        result = builder.run_command(self.argv, cwd=self.root, timeout=5)
        self.assertEqual(result['phase'], 'finished')
        self.assertEqual(self.marker.read_text(), 'x')
        with self.http.client.assessments.open_session(self.path) as session:
            self.assertEqual(session.store.document['capture']['state'], 'complete')
            self.assertEqual(session.store.document['finish']['state'], 'accepted')
            with patch('subprocess.Popen') as spawn:
                self.assertEqual(session.resume()['phase'], 'finished')
            spawn.assert_not_called()
        self.assertEqual(len(self.http.writes('admit')), 1)
        self.assertEqual(len(self.http.writes('finish')), 1)

    def test_lost_admission_replays_exact_intent_without_starting_claim_or_process(self):
        self.http.drop.add('admit')
        with self.assertRaises(SessionError) as error:
            self.run_command()
        self.assertEqual(error.exception.recovery_code, 'admission_unknown')
        self.assertEqual(error.exception.session_path, str(self.path))
        self.assertFalse(self.marker.exists())
        self.http.blocked = True  # Later readiness change cannot rebase saved admission.
        with self.http.client.assessments.open_session(self.path) as session:
            with patch('subprocess.Popen') as spawn:
                result = session.resume()
            spawn.assert_not_called()
            self.assertEqual(result['phase'], 'awaiting_harness')
        self.assertEqual(self.http.writes('admit')[0], self.http.writes('admit')[1])
        self.assertEqual(self.http.writes('next'), [])
        self.assertEqual(len(self.http.writes('prepare')), 1)

    def test_lost_claim_replays_only_same_private_claim_never_command(self):
        self.http.drop.add('next')
        with self.assertRaises(SessionError):
            self.run_command()
        with self.http.client.assessments.open_session(self.path) as session:
            with patch('subprocess.Popen') as spawn:
                session.resume()
            spawn.assert_not_called()
            self.assertEqual(session.store.document['execution']['state'], 'not_started')
        self.assertEqual(self.http.writes('next')[0], self.http.writes('next')[1])
        self.assertFalse(self.marker.exists())

    def test_lost_finish_recovers_exact_declaration_without_second_invocation(self):
        self.http.drop.add('finish')
        with self.assertRaises(SessionError):
            self.run_command()
        self.assertEqual(self.marker.read_text(), 'x')
        with self.http.client.assessments.open_session(self.path) as session:
            with patch('subprocess.Popen') as spawn:
                self.assertEqual(session.resume()['phase'], 'finished')
            spawn.assert_not_called()
            self.assertEqual(session.store.document['finish']['state'], 'accepted')
            with self.assertRaisesRegex(SessionError, 'session_finish_changed'):
                session.finish(agent_status='error')
        self.assertEqual(self.http.writes('finish')[0], self.http.writes('finish')[1])
        self.assertEqual(self.marker.read_text(), 'x')

    def test_lost_preflight_event_recovers_bytes_and_gap_without_inferred_finish(self):
        self.http.drop.add('transcript')
        with self.assertRaises(SessionError):
            self.run_command()
        first = self.http.writes('transcript')[0]
        with self.http.client.assessments.open_session(self.path) as session:
            with patch('subprocess.Popen') as spawn, patch('os.killpg') as kill:
                session.resume()
            spawn.assert_not_called()
            kill.assert_not_called()
            self.assertEqual(session.store.document['execution']['state'], 'exit_unknown')
            self.assertEqual(session.store.document['capture']['state'], 'incomplete')
            with self.assertRaisesRegex(SessionError, 'session_capture_incomplete'):
                session.finish(agent_status='completed')
        self.assertEqual(first, self.http.writes('transcript')[1])
        self.assertFalse(self.marker.exists())
        self.assertEqual(self.http.writes('finish'), [])


if __name__ == '__main__':
    unittest.main()
