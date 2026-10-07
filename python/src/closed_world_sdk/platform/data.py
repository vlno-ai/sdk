"""Organization retention and erasure controls."""
import copy
from datetime import datetime

from .. import SDKError
from .validation import _RUN, _match

def _data_policy(value):
    try:
        days, revision = value['retentionDays'], value['revision']
        if ((days is not None and (type(days) is not int or days not in (7, 30, 90)))
                or type(revision) is not int or revision < 0 or value['appliesTo'] != 'new_runs'
                or value['retentionStartsAt'] != 'run_terminal' or value['externalCopiesManaged'] is not False):
            raise ValueError()
        return copy.deepcopy(value)
    except (KeyError, TypeError, ValueError):
        raise SDKError('invalid_data_policy') from None


def _data_status(value, ident=None):
    try:
        _match(_RUN, value['runId'], 'product run ID')
        if ident is not None and value['runId'] != ident:
            raise ValueError()
        if (value['state'] not in ('retained', 'deleting', 'live_data_deleted')
                or value['backupDeletionIndividuallyVerified'] is not False
                or value['externalCopiesManaged'] is not False
                or type(value['canRequestDeletion']) is not bool):
            raise ValueError()
        for key in ('expiresAt', 'requestedAt', 'workerDeletedAt', 'archiveDeletedAt', 'databaseDeletedAt', 'backupExpiryEstimate'):
            if value[key] is not None:
                parsed = datetime.fromisoformat(value[key].replace('Z', '+00:00'))
                if parsed.tzinfo is None:
                    raise ValueError()
        if value['state'] != 'retained' and (value['requestedAt'] is None or value['canRequestDeletion']):
            raise ValueError()
        if value['state'] == 'live_data_deleted' and any(value[k] is None for k in ('workerDeletedAt', 'archiveDeletedAt', 'databaseDeletedAt')):
            raise ValueError()
        return copy.deepcopy(value)
    except (KeyError, TypeError, ValueError, AttributeError):
        raise SDKError('invalid_data_status', run_id=ident) from None


class PlatformData:
    """Retention and erasure controls. Mutations require an owner/admin credential."""
    def __init__(self, client):
        self._client = client

    def policy(self):
        return _data_policy(self._client._transport.request('GET', '/v1/world-data/policy'))

    def set_policy(self, retention_days, *, expected_revision):
        """Apply to newly created runs only. None means keep until explicitly deleted."""
        if ((retention_days is not None and (type(retention_days) is not int or retention_days not in (7, 30, 90)))
                or type(expected_revision) is not int or expected_revision < 0):
            raise ValueError('invalid retention policy')
        return _data_policy(self._client._transport.request('PUT', '/v1/world-data/policy',
            {'retentionDays': retention_days, 'expectedRevision': expected_revision}))

    def status(self, run_id):
        _match(_RUN, run_id, 'product run ID')
        return _data_status(self._client._transport.request('GET', f'/v1/world-runs/{run_id}/data'), run_id)

    def delete(self, run_id, *, confirm_run_id):
        """Request irreversible payload deletion; accepted does not mean completed."""
        _match(_RUN, run_id, 'product run ID')
        if confirm_run_id != run_id:
            raise ValueError('confirmation must match the run ID')
        return _data_status(self._client._transport.request('POST', f'/v1/world-runs/{run_id}/data/deletion',
            {'confirmRunId': confirm_run_id}), run_id)

    def deletions(self, *, limit=50, offset=0):
        if type(limit) is not int or not 1 <= limit <= 100 or type(offset) is not int or not 0 <= offset <= 1000000:
            raise ValueError('invalid pagination')
        value = self._client._transport.request('GET', f'/v1/world-data/deletions?limit={limit}&offset={offset}')
        try:
            if (not isinstance(value['items'], list) or len(value['items']) > limit
                    or value['limit'] != limit or value['offset'] != offset
                    or value['nextOffset'] is not None and value['nextOffset'] != offset + limit):
                raise ValueError()
            for item in value['items']:
                _data_status(item)
            return copy.deepcopy(value)
        except (KeyError, TypeError, ValueError):
            raise SDKError('invalid_data_deletions') from None

