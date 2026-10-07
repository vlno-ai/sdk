"""Safe customer recovery diagnostics, without raw server/process contents."""
from closed_world_sdk import SDKError
from .files import SessionFileError

FILE_CODES = {'session_corrupt', 'session_incomplete', 'session_in_use', 'session_closed',
              'session_platform_unsupported', 'session_filesystem_unsupported',
              'session_file_not_private', 'session_directory_not_private', 'session_path_changed',
              'session_file_too_large', 'session_filename_invalid', 'session_write_failed'}


def timeout_value(value):
    import math
    if type(value) not in (int, float) or not math.isfinite(value) or not 0 < value <= 3600:
        raise SDKError('session_invalid_timeout')
    return value


class SessionError(SDKError):
    def __init__(self, code, path, recovery_code):
        super().__init__(code)
        self.session_path = str(path)
        self.recovery_code = recovery_code


def failure(session, error):
    recovery = 'service_unavailable'
    try:
        document = session.store.document if session.store is not None else None
        if document:
            for name, state, code in (
                ('admission', 'unknown', 'admission_unknown'),
                ('claim', 'unknown', 'assignment_unknown'),
                ('execution', 'start_unknown', 'launch_unknown'),
                ('execution', 'running', 'exit_unknown'),
                ('capture', 'incomplete', 'capture_incomplete'),
                ('finish', 'unknown', 'finish_unknown'),
                ('cancellation', 'unknown', 'cancel_unknown')):
                if document[name]['state'] == state:
                    recovery = code
    except Exception:
        pass
    code = getattr(error, 'code', '')
    if code == 'session_identity_changed' or getattr(error, 'status', None) in (401, 403):
        recovery = 'authority_changed'
    elif code == 'session_source_changed':
        recovery = 'source_changed'
    elif code == 'session_connection_expired':
        recovery = 'connection_expired'
    safe = {'session_identity_changed', 'session_source_changed', 'session_scope_not_ready', 'session_finish_changed',
            'session_capture_incomplete', 'session_finish_not_permitted', 'session_admission_unconfirmed',
            'session_launch_not_permitted', 'session_command_contains_credentials',
            'session_connection_expired', 'session_closed', 'session_invalid_command', 'session_invalid_timeout',
            'transport_error_outcome_unknown', 'admission_outcome_unknown', 'session_cancel_outcome_unknown',
            'world_ready_timeout', 'run_wait_timeout', 'case_finish_timeout', 'outbox_full',
            'invalid_transcript_acknowledgement', 'invalid_platform_response', 'invalid_assessment_response'}
    if isinstance(error, SessionFileError) and str(error) in FILE_CODES:
        code = str(error)
    elif isinstance(error, FileExistsError):
        code = 'session_directory_exists'
    elif isinstance(error, FileNotFoundError):
        code = 'session_not_found' if session.store is None else 'session_storage_unavailable'
    elif isinstance(error, PermissionError):
        code = 'session_permission_denied'
    elif isinstance(error, TimeoutError):
        code = 'session_process_timeout'
    elif isinstance(error, OSError):
        code = 'session_storage_unavailable'
    elif code not in safe:
        code = 'session_operation_failed'
    result = SessionError(code, session.path, recovery)
    result.status = getattr(error, 'status', None)
    result.run_id = getattr(error, 'run_id', None)
    result.idempotency_key = getattr(error, 'idempotency_key', None)
    return result
