"""Credential redaction must cover JSON keys as well as string values."""
import json
import unittest
from types import SimpleNamespace
from unittest.mock import Mock

from closed_world_sdk.trajectory import TrajectoryRecorder


class TrajectoryCredentialTests(unittest.TestCase):
    def setUp(self):
        self.credentials = ('controller-secret-123', 'case-capability-456', 'claim-secret-789')
        run = SimpleNamespace(id='run', _client=SimpleNamespace(
            _transport=SimpleNamespace(api_key=self.credentials[0])))
        self.saved = []
        def upload(method, path, body):
            self.saved.extend(json.loads(json.dumps(body['events'])))
            return {'accepted': [event['id'] for event in body['events']]}
        run._request = Mock(side_effect=upload)
        case = SimpleNamespace(run_id='run', index=0, agent=SimpleNamespace(
            _transport=SimpleNamespace(api_key=self.credentials[1])))
        self.upload = run._request
        self.recorder = TrajectoryRecorder(run, case, self.credentials[2])

    def uploaded_data(self, data):
        self.recorder.emit('tool_result', data)
        return self.saved[-1]['data']

    def test_nested_credentials_in_keys_never_reach_upload(self):
        data = {'steps': [{f'field-{secret}': {'description': secret}}
                          for secret in self.credentials]}
        cleaned = self.uploaded_data(data)
        serialized = json.dumps(cleaned)
        for secret in self.credentials:
            self.assertNotIn(secret, serialized)
        self.assertEqual(cleaned['steps'][0], {'field-[redacted]': {'description': '[redacted]'}})

    def test_named_sensitive_fields_still_redact_values(self):
        cleaned = self.uploaded_data({'Authorization': 'opaque', 'api_key': 'opaque',
                                       'nested': {'message': 'ordinary task text'}})
        self.assertEqual(cleaned, {'Authorization': '[redacted]', 'api_key': '[redacted]',
                                  'nested': {'message': 'ordinary task text'}})


if __name__ == '__main__':
    unittest.main()
