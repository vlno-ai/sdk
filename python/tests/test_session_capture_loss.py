"""Post-spawn response loss preserves pending bytes until explicit recovery."""
import copy
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import uuid
from unittest.mock import patch

from vlno.sessions.store import SessionStore
from session_capture_fixtures import rig
from test_session_capture_store import ready_store


class CaptureLossTests(unittest.TestCase):
    def test_postspawn_loss_never_retries_from_cleanup_and_recovery_never_respawns(self):
        for lost_kind in ('stdout', 'capture_finished'):
            with self.subTest(lost_kind=lost_kind), tempfile.TemporaryDirectory() as root:
                path, marker = Path(root) / 'session', Path(root) / 'launches'
                script = (f'from pathlib import Path; import time; p=Path({str(marker)!r}); '
                          'p.write_text(p.read_text()+"x" if p.exists() else "x"); '
                          'print("ordinary output", flush=True); '
                          + ('time.sleep(30)' if lost_kind == 'stdout' else 'pass'))
                argv = [sys.executable, '-c', script]
                command = {'argv': argv, 'envKeys': [], 'cwd': root}
                requests, lost, children = [], [], []

                def upload(method, route, body):
                    event = copy.deepcopy(body['events'][0])
                    requests.append(event)
                    selected = (event['kind'] == lost_kind or
                                event['data'].get('event') == lost_kind)
                    if selected and not lost:
                        lost.append(event)
                        raise OSError('synthetic committed response loss')
                    return {'accepted': [event['id']]}

                original_spawn = subprocess.Popen

                def spawn(*args, **kwargs):
                    process = original_spawn(*args, **kwargs)
                    children.append(process)
                    return process

                with ready_store(str(path), command) as store:
                    store.update(lambda d: d['execution'].update(
                        state='start_unknown', launchId=str(uuid.uuid4())))
                    recorder, _, _ = rig(store)
                    recorder.run._request.side_effect = upload
                    with patch('subprocess.Popen', side_effect=spawn):
                        with self.assertRaisesRegex(OSError, 'synthetic committed response loss'):
                            recorder.record_process(argv, timeout=5)
                    capture = store.document['capture']
                    self.assertEqual(len(lost), 1)
                    self.assertEqual(requests.count(lost[0]), 1)
                    self.assertEqual(capture['state'], 'incomplete')
                    self.assertTrue(capture['gapRecorded'])
                    pending = capture['events'][capture['acknowledgedThrough']:]
                    self.assertEqual([row['event'] for row in pending][0], lost[0])
                    self.assertEqual([row['event']['kind'] for row in pending][-1], 'gap')
                    self.assertEqual(len(children), 1)
                    self.assertIsNotNone(children[0].poll())
                    saved_events = copy.deepcopy(capture['events'])

                with SessionStore.open(str(path)) as store:
                    recorder, _, replayed = rig(store)
                    with patch('subprocess.Popen') as spawn, patch('os.killpg') as kill:
                        recorder.recover()
                    spawn.assert_not_called()
                    kill.assert_not_called()
                    capture = store.document['capture']
                    self.assertEqual(replayed[0]['events'], [lost[0]])
                    self.assertEqual(capture['events'], saved_events)
                    self.assertEqual(capture['acknowledgedThrough'], len(saved_events))
                    self.assertEqual(capture['state'], 'incomplete')
                self.assertEqual(marker.read_text(), 'x')


if __name__ == '__main__':
    unittest.main()
