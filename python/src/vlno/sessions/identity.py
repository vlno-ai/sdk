"""Bind resumed client work to server identity without saving a workspace key."""

import copy
import re
from urllib.parse import urlsplit

from closed_world_sdk import SDKError


def endpoint_origin(endpoint):
    """Use one HTTP origin representation across Python and Go journals."""
    try:
        if (not isinstance(endpoint, str) or any(c.isspace() or ord(c) < 32 or ord(c) >= 127 for c in endpoint)
                or "?" in endpoint or "#" in endpoint):
            raise ValueError()
        parsed = urlsplit(endpoint)
        if (parsed.scheme not in {"https", "http"} or not parsed.hostname
                or parsed.username is not None or parsed.password is not None
                or parsed.path not in {"", "/"} or parsed.query or parsed.fragment):
            raise ValueError()
        host = parsed.hostname.lower()
        if not host.isascii() or "%" in host:
            raise ValueError()
        if ":" not in host:
            if len(host) > 253 or any(not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label)
                                     for label in host.split(".")):
                raise ValueError()
        if parsed.scheme == "http" and host not in {"localhost", "127.0.0.1", "::1"}:
            raise ValueError()
        port = parsed.port
        if port == 0 or parsed.netloc.endswith(":"):
            raise ValueError()
        default = 443 if parsed.scheme == "https" else 80
        host = "[" + host + "]" if ":" in host else host
        suffix = "" if port is None or port == default else ":" + str(port)
        return parsed.scheme + "://" + host + suffix
    except (ValueError, TypeError, AttributeError):
        raise SDKError("session_endpoint_invalid") from None


def identity(client):
    value = client._transport.request("GET", "/v1/assessment-runs/access")
    try:
        if (type(value) is not dict or set(value) != {"schema", "orgId", "actor"}
                or value["schema"] != "vlno.assessment-run-access/1"
                or type(value["orgId"]) is not str or not 1 <= len(value["orgId"]) <= 200):
            raise ValueError()
        actor = value["actor"]
        if (type(actor) is not dict or set(actor) != {"kind", "reference"}
                or actor["kind"] not in {"human", "service_key"}
                or type(actor["reference"]) is not str
                or not re.fullmatch(r"sa_[a-f0-9]{32}", actor["reference"])):
            raise ValueError()
    except (ValueError, KeyError, TypeError):
        raise SDKError("session_identity_invalid") from None
    return {"endpoint": endpoint_origin(client._transport.endpoint),
            "orgId": value["orgId"], "actor": copy.deepcopy(actor)}


def require_identity(client, expected):
    actual = identity(client)
    if actual != expected:
        raise SDKError("session_identity_changed")
    return actual
