"""Recovery cannot renew an intent, declare unseen completion, or spawn late."""
from unittest.mock import patch

from vlno.sessions.errors import SessionError
from session_workflow_fixtures import WorkflowCase
from session_capture_fixtures import rig
from closed_world_sdk.trajectory import TrajectoryRecorder


class RecoveryBoundaryTests(WorkflowCase):
    def test_lost_cancel_replays_its_actor_and_does_not_reclaim_or_finish(self):
        with self.http.builder(self.path) as session:
            session.connect()
            self.http.drop.add('cancel')
            with self.assertRaises(SessionError):
                session.cancel()
        with self.http.client.assessments.open_session(self.path) as session:
            before = session.store.document
            self.http.actor = {'kind': 'service_key', 'reference': 'sa_' + 'c' * 32}
            with self.assertRaisesRegex(SessionError, 'session_identity_changed'):
                session.resume()
            self.assertEqual(session.store.document, before)
            self.http.actor = self.http.original_actor
            self.assertEqual(session.resume()['phase'], 'cancelled')
        self.assertEqual(self.http.writes('cancel')[0], self.http.writes('cancel')[1])
        self.assertEqual(len(self.http.writes('next')), 1)
        self.assertEqual(self.http.writes('finish'), [])

    def test_unknown_finish_cannot_change_saved_declaration(self):
        self.http.drop.add('finish')
        with self.assertRaises(SessionError):
            self.run_command()
        with self.http.client.assessments.open_session(self.path) as session:
            before = session.store.document
            with self.assertRaisesRegex(SessionError, 'session_finish_changed'):
                session.finish(agent_status='timeout')
            self.assertEqual(session.store.document, before)
        self.assertEqual(len(self.http.writes('finish')), 1)

    def test_existing_supervised_session_cannot_start_again_even_if_not_started(self):
        self.http.drop.add('admit')
        with self.assertRaises(SessionError):
            self.run_command()
        with self.http.client.assessments.open_session(self.path) as session:
            with patch('subprocess.Popen') as spawn:
                with self.assertRaisesRegex(SessionError, 'session_launch_not_permitted'):
                    session.run_command(self.argv, cwd=self.root)
            spawn.assert_not_called()
        self.assertEqual(len(self.http.writes('admit')), 1)

    def test_preflight_upload_time_is_inside_process_deadline(self):
        durable, _, _ = rig()
        recorder = TrajectoryRecorder(durable.run, durable.case, durable.store.claim)
        with patch('closed_world_sdk.trajectory.time.monotonic', side_effect=[10, 12]):
            with patch('subprocess.Popen') as spawn:
                with self.assertRaises(TimeoutError):
                    recorder.record_process(['ordinary-harness'], timeout=1)
            spawn.assert_not_called()
        self.assertEqual(durable.run._request.call_count, 3)
