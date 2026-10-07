"""Customer client with additive approval-bound assessment operations."""

from closed_world_sdk.platform import PlatformClient

from .assessments import Assessments


class Client(PlatformClient):
    def __init__(self, endpoint, api_key, *, ca_file=None, timeout=200):
        super().__init__(endpoint, api_key, ca_file=ca_file, timeout=timeout)
        self.assessments = Assessments(self)
