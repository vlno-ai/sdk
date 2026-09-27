"""Connection manifests resolve only on the configured origin and scoped key."""
import copy
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('connection_test_base', Path(__file__).with_name('test_generation.py'))
base = importlib.util.module_from_spec(spec)
spec.loader.exec_module(base)
sdk = base.sdk
WORLD = 'f' * 32


class ConnectionTest(unittest.TestCase):
    setUp = base.GenerationTest.setUp
    tearDown = base.GenerationTest.tearDown

    def manifest(self):
        return {'schema_version': 1, 'world_id': WORLD, 'generation': 1, 'expires_at': 100,
                'task': 'Public task', 'mcp': {'path': f'/v1/worlds/{WORLD}/mcp',
                'headers': {'Authorization': 'Bearer ' + base.KEY}},
                'app': {'path': f'/v1/worlds/{WORLD}/call', 'headers': {'Authorization': 'Bearer ' + base.KEY}}}

    def test_normal_connection(self):
        self.reply = lambda *_: (200, self.manifest())
        app = sdk.App(self.client._transport.endpoint, WORLD, base.KEY)
        value = app.connection()
        self.assertEqual(value['mcp']['url'], self.client._transport.endpoint + f'/v1/worlds/{WORLD}/mcp')
        self.assertNotIn('path', value['mcp'])
        self.assertEqual(set(app.mcp), {'url', 'headers'})

    def test_invalid_connections(self):
        for field in ('world_id', 'path', 'headers'):
            value = self.manifest()
            if field == 'world_id':
                value[field] = '0' * 32
            elif field == 'path':
                value['mcp'][field] = 'https://foreign.example/mcp'
            else:
                value['mcp'][field] = {'Authorization': 'Bearer different'}
            self.reply = lambda *_, value=value: (200, copy.deepcopy(value))
            with self.subTest(field=field), self.assertRaises(sdk.SDKError):
                sdk.App(self.client._transport.endpoint, WORLD, base.KEY).connection()
