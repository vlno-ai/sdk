import unittest
from unittest.mock import Mock
from vlno import Client, SDKError


class HostedDataTests(unittest.TestCase):
    def setUp(self):
        self.client = Client('https://api.example.test', 'vlno_live_' + 'a' * 48)
        self.call = self.client._transport.request = Mock()
        self.id = 'cw_' + 'a' * 32
        self.policy = {'retentionDays': None, 'revision': 0, 'appliesTo': 'new_runs',
                       'retentionStartsAt': 'run_terminal', 'externalCopiesManaged': False}
        self.status = {'runId': self.id, 'state': 'deleting', 'requestedAt': '2026-09-29T12:00:00Z',
                       'workerDeletedAt': None, 'archiveDeletedAt': None, 'databaseDeletedAt': None,
                       'backupExpiryEstimate': None, 'expiresAt': None, 'canRequestDeletion': False,
                       'backupDeletionIndividuallyVerified': False, 'externalCopiesManaged': False}

    def test_policy_uses_explicit_revision_and_preserves_indefinite(self):
        self.call.return_value = self.policy
        self.assertEqual(self.client.data.policy(), self.policy)
        self.client.data.set_policy(None, expected_revision=0)
        self.call.assert_called_with('PUT', '/v1/world-data/policy', {'retentionDays': None, 'expectedRevision': 0})
        for days in [True, 1, -1, '30']:
            with self.assertRaises(ValueError):
                self.client.data.set_policy(days, expected_revision=0)

    def test_delete_requires_matching_confirmation_and_returns_pending(self):
        with self.assertRaises(ValueError):
            self.client.data.delete(self.id, confirm_run_id='different')
        self.call.assert_not_called()
        self.call.return_value = self.status
        self.assertEqual(self.client.data.delete(self.id, confirm_run_id=self.id)['state'], 'deleting')
        self.call.assert_called_with('POST', f'/v1/world-runs/{self.id}/data/deletion', {'confirmRunId': self.id})
        self.client.data.status(self.id)
        self.call.assert_called_with('GET', f'/v1/world-runs/{self.id}/data')

    def test_rejects_false_completion_foreign_run_and_unverified_backup_claims(self):
        for field, value in [('state', 'live_data_deleted'), ('runId', 'cw_' + 'b' * 32),
                             ('backupDeletionIndividuallyVerified', True), ('workerDeletedAt', 'yesterday')]:
            self.call.return_value = {**self.status, field: value}
            with self.subTest(field=field), self.assertRaises(SDKError):
                self.client.data.status(self.id)

    def test_deletion_pages_keep_status_and_cursor(self):
        self.call.return_value = {'items': [self.status], 'limit': 20, 'offset': 20, 'nextOffset': 40}
        page = self.client.data.deletions(limit=20, offset=20)
        self.assertEqual(page['nextOffset'], 40)
        self.call.assert_called_with('GET', '/v1/world-data/deletions?limit=20&offset=20')
        self.call.return_value['nextOffset'] = 10
        with self.assertRaises(SDKError):
            self.client.data.deletions(limit=20, offset=20)


class OperatorIntegrityFamilyTests(unittest.TestCase):
    def test_operator_client_accepts_integrity_family_without_hosted_privilege_changes(self):
        from closed_world_sdk import Client as WorkerClient
        client = WorkerClient('https://worker.example.test', 'a' * 40)
        client._transport.request = Mock(return_value={'id': 'a' * 32, 'state': 'validated'})
        client.scenarios.generate('archive-note-integrity', 'notes-workspace@1', 29,
                                  idempotency_key='integrity-test')
        args = client._transport.request.call_args
        self.assertEqual(args.args[2]['family'], 'archive-note-integrity')
        self.assertEqual(args.kwargs['key'], 'integrity-test')
