"""Organization-authenticated VLNO client; scoped connections go to the harness.

Public class imports stay compatible with the original platform module.
"""
import re
import time

from .. import Transport
from .agent import PlatformAgent, PlatformCase
from .data import PlatformData, _data_policy, _data_status
from .resources import PlatformRuns, PlatformSuites
from .run import PlatformRun
from .validation import _CASE_TERMINAL, _CLAIM, _REF, _RUN, _TERMINAL, _duration, _match, _view

class PlatformClient:
    """Customer API-key client for VLNO's organization-owned world runs."""
    def __init__(self, endpoint, api_key, *, ca_file=None, timeout=200):
        if not isinstance(api_key, str) or not re.fullmatch(r'vlno_live_[A-Za-z0-9_-]{20,256}', api_key):
            raise ValueError('a VLNO customer API key is required')
        self._transport = Transport(endpoint, api_key, ca_file=ca_file, timeout=_duration(timeout))
        self.runs = PlatformRuns(self)
        self.suites = PlatformSuites(self)
        self.data = PlatformData(self)

