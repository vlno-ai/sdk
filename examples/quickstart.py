"""Hosted Notes smoke test with a real captured subprocess, without a model."""
import argparse
import json
import os
from pathlib import Path
import secrets
import sys
from vlno import Client

parser=argparse.ArgumentParser()
parser.add_argument('--state',default='.vlno-demo-state.json')
parser.add_argument('--suite',default='pilot-notes@1')
parser.add_argument('--environment',action='store_true')
args=parser.parse_args()
state_path=Path(args.state)
# Refuse to overwrite recovery identity or automatically rerun a previous agent.
fd=os.open(state_path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
state={'request_key':secrets.token_hex(16),'claim':secrets.token_urlsafe(32),'phase':'create'}
with os.fdopen(fd,'w') as stream:
    json.dump(state,stream);stream.flush();os.fsync(stream.fileno())

def save():
    temporary=state_path.with_name(state_path.name+'.tmp')
    fd=os.open(temporary,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'w') as stream:
        json.dump(state,stream);stream.flush();os.fsync(stream.fileno())
    os.replace(temporary,state_path)

client=Client(os.environ.get('VLNO_ENDPOINT','https://api.vlno.ai'),os.environ['VLNO_API_KEY'])
run=client.runs.create(args.suite,revision='scripted-sdk-smoke-v0.13.0',idempotency_key=state['request_key'],environment=args.environment)
state['run_id']=run.id;save()
print('Watch live: https://beta.vlno.ai/runs/'+run.id,flush=True)
case=run.next(state['claim'],timeout=600)
if case is None: raise RuntimeError('No case assigned')
state.update(case_index=case.index,phase='assigned');save()
recorder=run.recorder(case,state['claim'])
recorder.emit('lifecycle',{'event':'scripted_smoke_test','message':'This example uses no model.'})
if args.environment:
    result=case.agent.exec(['python3','-c','print("VLNO remote shell capture check")'])
    if result.get('exit_code') != 0: raise RuntimeError('Remote shell check failed')
state['phase']='harness_started';save()
status='error'
try:
    code=recorder.record_process([sys.executable,str(Path(__file__).with_name('notes_harness.py'))],timeout=300)
    status='completed' if code==0 else 'error'
except TimeoutError:
    status='timeout'
finally:
    # A recorder failure is a harness failure, not a fabricated task pass.
    state.update(phase='finishing',agent_status=status);save()
    run.finish(case.index,state['claim'],agent_status=status,timeout=600)
result=run.wait(timeout=600)
state['phase']='finished';save()
print(json.dumps({'run_id':run.id,'evaluation':result['evaluationStatus'],'cleanup':result['summary']['cleanup']['status']},indent=2))
if result['evaluationStatus']!='passed' or result['summary']['cleanup']['status']!='confirmed':
    raise SystemExit(1)
