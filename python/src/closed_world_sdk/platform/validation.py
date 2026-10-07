"""Validate lifecycle envelopes without inventing absent worker results."""
import math
import re

from .. import SDKError

_RUN = re.compile(r'cw_[a-f0-9]{32}')
_CLAIM = re.compile(r'[A-Za-z0-9_-]{32,128}')
_REF = re.compile(r'[a-z][a-z0-9-]{0,47}@[1-9][0-9]{0,8}')
_TERMINAL = {'completed', 'failed', 'cancelled'}
_CASE_TERMINAL = {'passed', 'failed', 'error', 'inconclusive', 'invalid', 'not_run'}
def _match(pattern, value, label):
    if not isinstance(value, str) or not pattern.fullmatch(value):
        raise ValueError('invalid ' + label)
    return value


def _duration(value):
    if isinstance(value, bool) or not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
        raise ValueError('timeout must be a positive finite number')
    return value


def _view(value, ident=None):
    if (not isinstance(value, dict) or not isinstance(value.get('id'), str)
            or not _RUN.fullmatch(value['id']) or ident is not None and value['id'] != ident
            or value.get('engine') != 'closed_world'
            or value.get('state') not in _TERMINAL | {'provisioning', 'pending', 'running', 'cancelling'}):
        raise SDKError('invalid_platform_response', run_id=ident)
    result = value.get('result')
    if result is not None:
        if (not isinstance(result, dict) or result.get('schema') != 'vlno.world-run/1'
                or result.get('engine') != 'closed_world' or result.get('executionState') != value['state']
                or result.get('evaluationStatus') not in {'not_evaluated', 'passed', 'failed', 'inconclusive', 'invalid'}
                or not isinstance(result.get('cases'), list) or not 1 <= len(result['cases']) <= 16):
            raise SDKError('invalid_platform_response', run_id=value['id'])
        for index, case in enumerate(result['cases']):
            if (not isinstance(case, dict) or type(case.get('index')) is not int or case['index'] != index
                    or case.get('state') not in _CASE_TERMINAL | {'pending', 'running', 'finishing'}):
                raise SDKError('invalid_platform_response', run_id=value['id'])
    elif value['state'] != 'provisioning':
        raise SDKError('invalid_platform_response', run_id=value['id'])
    return value

