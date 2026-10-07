"""Validate and expose only the case-scoped app connection."""
import copy
import math
import re
import time

from .. import SDKError, Transport
from .validation import _REF

class PlatformAgent:
    """Leased app/MCP/optional shell access. Contains no customer account key."""
    def __init__(self, transport, world_id, manifest):
        self._transport = transport
        self.world_id = world_id
        self._manifest = manifest
        self._path = f'/v1/world-agent/{world_id}'

    def connection(self):
        """Private manifest: do not log it or put it in a model message."""
        return copy.deepcopy(self._manifest)

    @property
    def mcp(self):
        entry = self._manifest['mcp']
        return {'url': entry['url'], 'headers': dict(entry['headers'])}

    def operations(self):
        return self._transport.request('GET', self._path + '/operations')

    def call(self, operation, /, **arguments):
        return self._transport.request('POST', self._path + '/call', {'operation': operation, 'arguments': arguments})

    def exec(self, argv, *, cwd='/home/model', timeout_seconds=30):
        if 'environment' not in self._manifest:
            raise ValueError('shell access is not enabled for this case')
        return self._transport.request('POST', self._path + '/exec',
                                       {'argv': argv, 'cwd': cwd, 'timeout_seconds': timeout_seconds})


class PlatformCase:
    def __init__(self, client, run_id, value):
        try:
            world = value['worldId']
            if not isinstance(world, str) or not re.fullmatch(r'[a-f0-9]{32}', world):
                raise ValueError()
            if (type(value['index']) is not int or not 0 <= value['index'] < 16 or not isinstance(value['task'], str)
                    or not isinstance(value['scenario'], str) or not _REF.fullmatch(value['scenario'])
                    or isinstance(value['expiresAt'], bool) or not isinstance(value['expiresAt'], (int, float))
                    or not math.isfinite(value['expiresAt']) or value['expiresAt'] <= time.time()):
                raise ValueError()
            manifest = copy.deepcopy(value['connection'])
            if (type(manifest['schema_version']) is not int or manifest['schema_version'] != 1
                    or type(manifest['generation']) is not int or manifest['generation'] != 1
                    or manifest['world_id'] != world or manifest['task'] != value['task']
                    or manifest['expires_at'] != value['expiresAt']):
                raise ValueError()
            authorization = manifest['mcp']['headers']['Authorization']
            if not isinstance(authorization, str) or not re.fullmatch(r'Bearer [A-Za-z0-9_-]{32,128}', authorization):
                raise ValueError()
            token = authorization[7:]
            if token == client._transport.api_key:
                raise ValueError()
            for name, suffix in [('mcp', 'mcp'), ('app', 'call'), ('environment', 'exec')]:
                if name == 'environment' and name not in manifest:
                    continue
                entry = manifest[name]
                path = f'/v1/world-agent/{world}/{suffix}'
                if (entry.get('path') != path or 'url' in entry
                        or entry.get('headers') != {'Authorization': authorization}):
                    raise ValueError()
                entry['url'] = client._transport.endpoint + entry.pop('path')
            if manifest['mcp'].get('transport') != 'streamable-http':
                raise ValueError()
        except (KeyError, TypeError, ValueError, AttributeError):
            raise SDKError('invalid_connection_manifest', run_id=run_id) from None
        self.run_id = run_id
        self.index, self.scenario, self.task = value['index'], value['scenario'], value['task']
        self.world_id, self.expires_at = world, value['expiresAt']
        scoped = Transport(client._transport.endpoint, token,
                           ca_file=client._transport.ca_file, timeout=client._transport.timeout)
        self.agent = PlatformAgent(scoped, world, manifest)

