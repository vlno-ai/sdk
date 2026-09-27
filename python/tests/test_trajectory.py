import json
import os
import sys
import unittest
from unittest.mock import Mock
from closed_world_sdk import SDKError
from closed_world_sdk.platform import PlatformClient, PlatformCase
from test_platform import assigned, view, RUN, KEY, TOKEN, CLAIM

class TrajectoryTests(unittest.TestCase):
 def setUp(self):
  self.client=PlatformClient('https://api.example.test',KEY)
  self.client._transport.request=Mock(return_value=view())
  self.run=self.client.runs.create('test-suite@1','rev',idempotency_key='stable-key-123')
  self.case=PlatformCase(self.client,RUN,assigned()['case'])
  self.saved=[]
  def upload(method,path,body=None,**kwargs):
   self.saved.extend(body['events'])
   return {'accepted':[event['id'] for event in body['events']]}
  self.client._transport.request=Mock(side_effect=upload)
  self.rec=self.run.recorder(self.case,CLAIM)
 def test_redacts_known_credentials_and_chunks_messages(self):
  self.rec.message('assistant','a'*2040+TOKEN+' '+KEY+' '+CLAIM+' z'*4096)
  content=''.join(e['data']['content'] for e in self.saved)
  for secret in [TOKEN,KEY,CLAIM]: self.assertNotIn(secret,content)
  self.assertGreater(len(self.saved),1)
  self.assertTrue(self.saved[-1]['data']['last'])
 def test_retry_preserves_id_and_does_not_duplicate(self):
  original=self.client._transport.request.side_effect
  self.client._transport.request.side_effect=SDKError('http_error')
  with self.assertRaises(SDKError): self.rec.emit('message',{'role':'assistant','content':'hello'})
  ident=self.rec._pending[0]['id']
  self.client._transport.request.side_effect=original
  self.rec.flush()
  self.assertEqual([e['id'] for e in self.saved],[ident])
 def test_process_captures_both_streams_without_controller_environment(self):
  with unittest.mock.patch.dict(os.environ,{'VLNO_API_KEY':KEY}):
   code=self.rec.record_process([sys.executable,'-c',"import os,sys; print('hello agent'); print('diagnostic',file=sys.stderr); assert 'VLNO_API_KEY' not in os.environ"],timeout=5)
  self.assertEqual(code,0)
  self.assertIn('hello agent',''.join(e['data'].get('text','') for e in self.saved if e['kind']=='stdout'))
  self.assertIn('diagnostic',''.join(e['data'].get('text','') for e in self.saved if e['kind']=='stderr'))
  self.assertEqual(self.saved[-1]['data']['event'],'capture_finished')
 def test_split_secret_is_redacted_and_unicode_preserved(self):
  text='é'*2040+TOKEN+' '+'🌏'*3000
  script='import sys; sys.stdout.write('+repr(text)+')'
  self.rec.record_process([sys.executable,'-c',script],timeout=5)
  captured=''.join(e['data'].get('text','') for e in self.saved if e['kind']=='stdout')
  self.assertEqual(captured,text.replace(TOKEN,'[redacted]'))
 def test_timeout_records_gap_and_kills_process(self):
  with self.assertRaises(TimeoutError): self.rec.record_process([sys.executable,'-c','import time; time.sleep(30)'],timeout=.1)
  self.assertTrue(any(e['kind']=='gap' for e in self.saved))
 def test_foreign_case_rejected(self):
  self.case.run_id='cw_'+'f'*32
  with self.assertRaises(ValueError): self.run.recorder(self.case,CLAIM)
