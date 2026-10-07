"""Loopback product-protocol double. No worker, model, app or remote API."""
import copy
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
from pathlib import Path
import socket
import threading

from vlno import Client
from assessment_fixtures import ASSESSMENT, REVISION, SOURCE, admitted, readiness, status, connection
from test_platform import KEY, RUN, view


class SessionHTTP:
    def __init__(self):
        self.actor = {'kind': 'service_key', 'reference': 'sa_' + 'a' * 32}
        self.original_actor = copy.deepcopy(self.actor)
        self.org, self.drop, self.calls = 'org-fixture', set(), []
        self.receipts, self.events = {}, {}
        self.claim, self.finish_body = None, None
        self.phase, self.version = 'awaiting_harness', 1
        self.ready = connection()
        self.blocked, self.drift, self.denied = False, False, False
        fixture = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_):
                pass

            def do_GET(self):
                self.respond()

            def do_POST(self):
                self.respond()

            def respond(self):
                length = int(self.headers.get('Content-Length', '0'))
                body = json.loads(self.rfile.read(length)) if length else None
                operation = fixture.operation(self.command, self.path)
                key = self.headers.get('Idempotency-Key')
                fixture.calls.append((operation, copy.deepcopy(body), key))
                if fixture.denied:
                    self.send_response(403)
                    self.end_headers()
                    self.wfile.write(b'{"error":{"code":"run_access_changed"}}')
                    return
                value = fixture.reply(operation, body, key)
                if operation in fixture.drop:
                    fixture.drop.remove(operation)
                    self.connection.shutdown(socket.SHUT_RDWR)
                    self.connection.close()
                    return
                raw = json.dumps(value).encode()
                self.send_response(200)
                self.send_header('Content-Length', str(len(raw)))
                self.send_header('Content-Type', 'application/json')
                self.end_headers()
                self.wfile.write(raw)

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.client = Client(f'http://127.0.0.1:{self.server.server_port}', KEY)

    @staticmethod
    def operation(method, path):
        if path.endswith('/assessment-runs/access'):
            return 'identity'
        if path.endswith('/run-preparation'):
            return 'prepare'
        if path.endswith('/assessment-run'):
            return 'status'
        if method == 'POST' and path.endswith('/runs'):
            return 'admit'
        if method == 'GET':
            return 'lifecycle'
        return path.rsplit('/', 1)[-1]

    def lifecycle(self):
        terminal = self.phase in {'finished', 'cancelled'}
        state = 'cancelled' if self.phase == 'cancelled' else 'completed' if terminal else 'running'
        value = view(state, 'inconclusive' if terminal else 'not_evaluated', 'not_run' if terminal else 'running')
        value['result']['cases'][0]['scenario'] = 'archive-note@1'
        value['cancellationRequested'] = self.phase == 'cancelled'
        return value

    def reply(self, operation, body, key):
        if operation == 'identity':
            return {'schema': 'vlno.assessment-run-access/1', 'orgId': self.org, 'actor': self.actor}
        if operation == 'prepare':
            value = readiness(not self.blocked)
            value.update(orgId=self.org, actor=self.actor)
            return value
        if operation == 'admit':
            if key not in self.receipts:
                value = admitted()
                value.update(idempotencyKey=key, actor=copy.deepcopy(self.actor), submitted=body)
                self.receipts[key] = value
            return self.receipts[key]
        if operation == 'lifecycle':
            return self.lifecycle()
        if operation == 'status':
            value = status()
            value.update(admittedBy=self.original_actor, version=self.version, phase=self.phase)
            if self.drift:
                value['source']['systemRevisionId'] = '99999999-9999-4999-8999-999999999999'
            if self.phase in {'finished', 'cancelled'}:
                value.update(cleanup='confirmed', actions={'canClaim': False, 'canCancel': False})
            return value
        if operation == 'next':
            self.claim = self.claim or body['claim']
            assert self.claim == body['claim']
            self.phase, self.version = 'running', max(2, self.version)
            return {**copy.deepcopy(self.ready), 'run': self.lifecycle()}
        if operation == 'transcript':
            assert body['claim'] == self.claim
            event = body['events'][0]
            assert event['id'] not in self.events or self.events[event['id']] == event
            self.events[event['id']] = event
            return {'accepted': [event['id']]}
        if operation == 'finish':
            assert body['claim'] == self.claim
            self.finish_body = self.finish_body or body
            assert self.finish_body == body
            self.phase, self.version = 'finished', self.version + 1
            return self.lifecycle()
        if operation == 'cancel':
            self.phase, self.version = 'cancelled', self.version + 1
            return self.lifecycle()
        raise AssertionError('unexpected fixture operation')

    def builder(self, path):
        return self.client.assessments.session(path, assessment_id=ASSESSMENT, plan_revision_id=REVISION)

    def writes(self, operation):
        return [call for call in self.calls if call[0] == operation]

    def close(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
