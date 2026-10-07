"""Fresh identity, immutable commands, explicit declarations and safe resume."""
import copy
from unittest.mock import patch

from vlno.sessions.errors import SessionError
from session_workflow_fixtures import WorkflowCase
from test_platform import KEY


class WorkflowBoundaryTests(WorkflowCase):
    def test_prepared_resume_never_submits_new_admission(self):
        with self.http.builder(self.path) as session:
            session._create('external')  # Cut after local journal, before durable POST intent.
        before = list(self.http.calls)
        with self.http.client.assessments.open_session(self.path) as session:
            with self.assertRaisesRegex(SessionError, 'session_admission_unconfirmed'):
                session.resume()
            self.assertEqual(session.store.document['admission']['state'], 'prepared')
        self.assertEqual(self.http.calls, before)

    def test_changed_actor_blocks_unknown_replay_without_rewriting_intent(self):
        self.http.drop.add('admit')
        with self.assertRaises(SessionError):
            self.run_command()
        self.http.actor = {'kind': 'service_key', 'reference': 'sa_' + 'c' * 32}
        with self.http.client.assessments.open_session(self.path) as session:
            before = session.store.document
            with self.assertRaisesRegex(SessionError, 'session_identity_changed'):
                session.resume()
            self.assertEqual(session.store.document, before)
        self.assertEqual(len(self.http.writes('admit')), 1)

    def test_current_source_contradiction_cannot_authorize_finish(self):
        with self.http.builder(self.path) as session:
            session.connect()
            self.http.drift = True
            with self.assertRaisesRegex(SessionError, 'session_source_changed'):
                session.finish(agent_status='completed')
            self.assertEqual(session.store.document['finish']['state'], 'not_started')
        self.assertEqual(self.http.writes('finish'), [])

    def test_external_completed_is_explicit_and_does_not_claim_complete_transcript(self):
        with self.http.builder(self.path) as session:
            assigned = session.connect()
            self.assertEqual(assigned.index, 0)
            self.assertEqual(self.http.writes('finish'), [])
            result = session.finish(agent_status='completed')
            self.assertEqual(result['phase'], 'finished')
            document = session.store.document
            self.assertEqual(document['execution']['state'], 'external_active')
            self.assertIsNone(document['execution']['exitCode'])
            self.assertEqual(document['capture']['state'], 'incomplete')
            self.assertEqual(document['finish']['command']['agent_status'], 'completed')
        self.assertFalse(self.marker.exists())

    def test_context_exit_only_releases_lock_and_authorized_reader_can_cancel_as_self(self):
        with self.http.builder(self.path) as session:
            session.connect()
        self.assertEqual(self.http.writes('finish'), [])
        self.assertEqual(self.http.writes('cancel'), [])
        replacement = {'kind': 'service_key', 'reference': 'sa_' + 'c' * 32}
        self.http.actor = replacement
        with self.http.client.assessments.open_session(self.path) as session:
            self.assertEqual(session.status()['phase'], 'running')
            self.assertEqual(session.cancel()['phase'], 'cancelled')
            self.assertEqual(session.store.document['cancellation']['actor'], replacement)
            self.assertNotEqual(session.store.document['claim']['actor'], replacement)

    def test_wrong_org_or_endpoint_never_rebases_status(self):
        with self.http.builder(self.path) as session:
            session.connect()
        with self.http.client.assessments.open_session(self.path) as session:
            self.http.org = 'different-org'
            before = session.store.document
            with self.assertRaisesRegex(SessionError, 'session_identity_changed'):
                session.status()
            self.http.org = 'org-fixture'
            self.http.client._transport.endpoint = 'https://other.example.test'
            with self.assertRaisesRegex(SessionError, 'session_identity_changed'):
                session.status()
            self.assertEqual(session.store.document, before)

    def test_revoked_identity_stops_replay_without_second_post(self):
        self.http.drop.add('next')
        with self.assertRaises(SessionError):
            self.run_command()
        self.http.denied = True
        with self.http.client.assessments.open_session(self.path) as session:
            with self.assertRaises(SessionError) as error:
                session.resume()
            self.assertNotIn(KEY, str(error.exception))
        self.assertEqual(len(self.http.writes('next')), 1)

    def test_known_secret_in_command_is_rejected_before_storage_and_network(self):
        with self.assertRaisesRegex(SessionError, 'session_command_contains_credentials'):
            self.http.builder(self.path).run_command(['echo', KEY], cwd=self.root)
        self.assertFalse(self.path.exists())
        self.assertEqual(self.http.calls, [])

    def test_missing_existing_session_does_not_create_or_call_prepare(self):
        with self.assertRaises(Exception):
            self.http.client.assessments.open_session(self.path)
        self.assertFalse(self.path.exists())
        self.assertEqual(self.http.calls, [])

    def test_fsync_failure_before_admission_intent_stops_dispatch(self):
        with self.http.builder(self.path) as session:
            session._create('external')
            with patch('vlno.sessions.files.os.fsync', side_effect=OSError('synthetic private detail')):
                with self.assertRaises(SessionError) as error:
                    session.connect()
            self.assertNotIn('private detail', str(error.exception))
            self.assertEqual(self.http.writes('admit'), [])
            with self.assertRaises(SessionError):
                session.connect()
            self.assertEqual(self.http.writes('admit'), [])
