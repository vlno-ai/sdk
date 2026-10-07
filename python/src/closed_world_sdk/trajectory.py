"""Optional trusted harness recorder, independent of any model/provider.

Events describe what the harness reports; they are not evaluation evidence.
Process capture runs only the caller-supplied command and never retries it.
"""
import codecs
import datetime
import json
import os
import queue
import re
import signal
import subprocess
import threading
import time
import uuid

from . import SDKError

KINDS = {'message', 'tool_call', 'tool_result', 'shell_command', 'shell_output', 'stdout', 'stderr', 'lifecycle', 'gap'}

class TrajectoryRecorder:
    def __init__(self, run, case, claim):
        if case.run_id != run.id:
            raise ValueError('case belongs to another run')
        self.run, self.case, self._claim = run, case, claim
        self._secrets = [run._client._transport.api_key, case.agent._transport.api_key, claim]
        self._pending = []
        self._lock = threading.RLock()

    def _clean(self, value):
        if isinstance(value, str):
            for secret in self._secrets:
                value = value.replace(secret, '[redacted]')
            value = re.sub(r'Bearer\s+[A-Za-z0-9._~+/=-]+', 'Bearer [redacted]', value, flags=re.I)
            return re.sub(r'vlno_live_[A-Za-z0-9_-]+', '[redacted]', value)
        if isinstance(value, list):
            return [self._clean(v) for v in value]
        if isinstance(value, dict):
            return {self._clean(k): '[redacted]' if re.fullmatch(r'authorization|cookie|set-cookie|password|passwd|secret|api[-_]?key|access[-_]?token|refresh[-_]?token|token|claim', k, re.I) else self._clean(v) for k,v in value.items()}
        return value

    def emit(self, kind, data, *, event_id=None, occurred_at=None):
        """Append one event. flush() retries the same ID after a lost reply.

        An upload failure raises; it never silently discards content or reruns
        the harness. Callers must finish recording before run.finish().
        """
        if kind not in KINDS or not isinstance(data, dict):
            raise ValueError('invalid trajectory event')
        event = {'id': str(uuid.UUID(event_id)) if event_id else str(uuid.uuid4()), 'kind': kind,
                 'occurredAt': occurred_at or datetime.datetime.now(datetime.timezone.utc).isoformat().replace('+00:00','Z'),
                 'data': self._clean(data)}
        if len(json.dumps(event, ensure_ascii=False).encode()) > 24000:
            raise ValueError('event too large; stream output in smaller chunks')
        with self._lock:
            if self._pending:
                self.flush()
            self._pending.append(event)
            self.flush()
        return event['id']

    def flush(self):
        with self._lock:
            if not self._pending:
                return
            reply = self.run._request('POST', f'/cases/{self.case.index}/transcript',
                                      {'claim': self._claim, 'events': self._pending})
            if reply.get('accepted') != [event['id'] for event in self._pending]:
                raise SDKError('invalid_transcript_acknowledgement', run_id=self.run.id)
            self._pending.clear()

    def message(self, role, content):
        if role not in {'user', 'assistant', 'system', 'tool'} or not isinstance(content, str):
            raise ValueError('invalid message')
        # Long messages remain explicitly connected, not truncated.
        content = self._clean(content)
        ident = str(uuid.uuid4())
        for part, offset in enumerate(range(0, max(1,len(content)), 2048)):
            self.emit('message', {'role': role, 'content': content[offset:offset+2048],
                                  'messageId': ident, 'part': part, 'last': offset+2048 >= len(content)})

    def record_process(self, argv, *, input=None, env_keys=(), timeout=300):
        """Record a local harness's stdout/stderr live, including its native JSON.

        Enable that harness's normal transcript/stream output for conversation
        and internal tool messages. Unemitted private state is not observable.
        The child gets an allowlisted environment and no platform key or claim.
        This is a process recorder, not a sandbox for the customer harness.
        """
        if (not isinstance(argv, list) or not argv or not all(isinstance(v,str) and '\0' not in v for v in argv)
                or isinstance(timeout,bool) or not isinstance(timeout,(int,float)) or not 0 < timeout <= 3600):
            raise ValueError('invalid command or timeout')
        env = {}
        for name in ('PATH','LANG','TMPDIR', *env_keys):
            if not isinstance(name,str) or not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*',name):
                raise ValueError('invalid environment variable name')
            if name.upper().startswith(('VLNO_','CLOSED_WORLD_','CW_','WORKER_')) or 'CLAIM' in name.upper():
                raise ValueError('control credentials cannot be passed to the child')
            value = os.environ.get(name)
            if value is not None and not any(secret in value for secret in self._secrets):
                env[name] = value
        self.emit('lifecycle', {'event':'capture_started','streams':['stdout','stderr'],
                               'conversation':'Only messages emitted by the harness are recorded.'})
        self.message('user', self.case.task)
        self.emit('shell_command', {'argv':argv, 'location':'customer harness'})
        payload = json.dumps(input if input is not None else {'task':self.case.task,'connection':self.case.agent.connection()}).encode()+b'\n'
        if len(payload)>4*1024*1024:
            raise ValueError('harness input too large')
        process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   env=env, start_new_session=os.name=='posix')
        stream = queue.Queue(maxsize=32)
        stop = threading.Event()
        def put(item):
            while not stop.is_set():
                try:
                    stream.put(item,timeout=.1)
                    return
                except queue.Full:
                    pass
        def drain(pipe, name):
            decoder = codecs.getincrementaldecoder('utf-8')('replace')
            try:
                while True:
                    chunk = os.read(pipe.fileno(),4096)
                    if not chunk:
                        tail=decoder.decode(b'',final=True)
                        if tail: put((name,tail))
                        break
                    put((name,decoder.decode(chunk)))
            finally:
                put((name,None))
                pipe.close()
        def write_input():
            try:
                process.stdin.write(payload)
                process.stdin.close()
            except (BrokenPipeError,OSError):
                pass
        threads=[threading.Thread(target=drain,args=(process.stdout,'stdout'),daemon=True),
                 threading.Thread(target=drain,args=(process.stderr,'stderr'),daemon=True),
                 threading.Thread(target=write_input,daemon=True)]
        for thread in threads: thread.start()
        pending={'stdout':'','stderr':''}
        finished=set()
        deadline=time.monotonic()+timeout
        try:
            while len(finished)<2:
                if time.monotonic()>=deadline:
                    raise TimeoutError('harness process exceeded its deadline')
                try: name,text=stream.get(timeout=min(.1,max(.001,deadline-time.monotonic())))
                except queue.Empty: continue
                if text is None: finished.add(name)
                else: pending[name]+=text
                # Keep a tail larger than any known credential so split writes
                # cannot expose it. Newline-terminated messages stream immediately.
                while pending[name] and (name in finished or '\n' in pending[name] or len(pending[name])>4096):
                    newline=pending[name].find('\n')
                    size=min(2048,newline+1 if newline>=0 else max(1,len(pending[name])-2048) if name not in finished else len(pending[name]))
                    for secret in self._secrets:
                        position=pending[name].find(secret)
                        while position>=0:
                            if position<size<position+len(secret): size=position
                            position=pending[name].find(secret,position+len(secret))
                    for match in re.finditer(r'(?:Bearer\s+[A-Za-z0-9._~+/=-]{1,512}|vlno_live_[A-Za-z0-9_-]{1,128})',pending[name],re.I):
                        if match.start()<size<match.end(): size=match.start()
                    if size==0:
                        # A recognized token at the chunk boundary fits within
                        # the retained tail and can be redacted as a whole.
                        size=min(len(pending[name]),1024)
                    self.emit(name, {'text':pending[name][:size]})
                    pending[name]=pending[name][size:]
            code=process.wait(timeout=max(.001,deadline-time.monotonic()))
            self.emit('lifecycle', {'event':'capture_finished','exitCode':code,'streams':['stdout','stderr']})
            return code
        except BaseException:
            try: self.emit('gap', {'message':'Harness capture stopped before a complete process transcript was confirmed.'})
            except Exception: pass
            raise
        finally:
            stop.set()
            if os.name=='posix':
                try: os.killpg(process.pid,signal.SIGKILL)
                except ProcessLookupError: pass
            elif process.poll() is None: process.kill()
            process.wait(timeout=3)
            for thread in threads: thread.join(timeout=1)
