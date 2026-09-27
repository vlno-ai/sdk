"""Scripted Notes smoke test. No model; receives only the scoped connection."""
import json
import sys
import urllib.request

request = json.load(sys.stdin)
connection = request['connection']
task = request['task']
app = connection['app']
# The connection is prepared by the trusted SDK controller. Never print headers.
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
def call(operation, **arguments):
    body = json.dumps({'operation': operation, 'arguments': arguments}).encode()
    req = urllib.request.Request(app['url'], body, {**app['headers'], 'Content-Type':'application/json'}, method='POST')
    with opener.open(req, timeout=120) as response:
        return json.load(response)
print('Scripted smoke test: inspecting notes (no model).', flush=True)
notes = call('list_notes')['body']['items']
targets = [note for note in notes if note.get('text') and note['text'] in task]
if len(targets) != 1:
    raise RuntimeError('Task did not identify exactly one note; no mutation attempted')
print('The task identifies one note. Archiving it now.', flush=True)
reply = call('archive_note', note_id=targets[0]['id'], archived=True)
if reply['status'] != 200:
    raise RuntimeError('Archive response was not successful')
print('Archive call returned successfully; independent grading follows.', flush=True)
