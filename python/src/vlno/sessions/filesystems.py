"""Recognize the two local filesystem families qualified for durable journals."""

import ctypes
import ctypes.util
from pathlib import Path
import sys


def filesystem_type(descriptor):
    if sys.platform == "darwin":
        library = ctypes.CDLL(ctypes.util.find_library("c"), use_errno=True)
        function = library.fstatfs
        function.argtypes = (ctypes.c_int, ctypes.c_void_p)
        function.restype = ctypes.c_int
        # Darwin statfs: fixed-width fields through f_fssubtype total72 bytes;
        # f_fstypename is the following16 bytes. Buffer exceeds the full struct.
        buffer = ctypes.create_string_buffer(4096)
        if function(descriptor, buffer) != 0:
            raise OSError(ctypes.get_errno(), "session filesystem inspection failed")
        flags = int.from_bytes(buffer.raw[64:68], sys.byteorder)
        if not flags & 0x1000:  # Darwin MNT_LOCAL
            return "unsupported"
        return buffer.raw[72:88].split(b"\0", 1)[0].decode("ascii")
    if sys.platform == "linux":
        # Match the opened descriptor's mount, not the caller-supplied path.
        info = Path(f"/proc/self/fdinfo/{descriptor}").read_text()
        mount = next(line.split()[1] for line in info.splitlines() if line.startswith("mnt_id:"))
        with Path("/proc/self/mountinfo").open() as stream:
            content = stream.read(2 * 1024 * 1024 + 1)
        if len(content) > 2 * 1024 * 1024:
            raise ValueError("mount information too large")
        for line in content.splitlines():
            left, right = line.split(" - ", 1)
            if left.split()[0] == mount:
                return right.split()[0]
    return "unsupported"


def supported_filesystem(descriptor):
    try:
        return filesystem_type(descriptor) in {"apfs", "ext4"}
    except (OSError, ValueError, IndexError, StopIteration, AttributeError):
        return False
