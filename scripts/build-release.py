"""Build client-only release artifacts. Run after tests with setuptools installed."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import zipfile

root=Path(__file__).resolve().parents[1]
out=Path(sys.argv[1]).absolute();out.mkdir(parents=True,exist_ok=True)
version='0.12.0'
subprocess.run([sys.executable,'-c',"import setuptools.build_meta; setuptools.build_meta.build_wheel("+repr(str(out))+")"],cwd=root/'python',check=True)
for target,arch in [('darwin','arm64'),('darwin','amd64'),('linux','amd64'),('linux','arm64'),('windows','amd64')]:
    import tempfile
    with tempfile.TemporaryDirectory() as temporary:
        binary=Path(temporary)/('vlno.exe' if target=='windows' else 'vlno')
        subprocess.run(['go','build','-trimpath','-ldflags=-s -w','-o',str(binary),'./cmd/vlno'],cwd=root/'cli',env={**os.environ,'GOOS':target,'GOARCH':arch,'CGO_ENABLED':'0'},check=True)
        name=f'vlno_{version}_{target}_{arch}'
        if target=='windows':
            with zipfile.ZipFile(out/(name+'.zip'),'w',zipfile.ZIP_DEFLATED) as archive:archive.write(binary,binary.name)
        else:
            with tarfile.open(out/(name+'.tar.gz'),'w:gz') as archive:archive.add(binary,arcname=binary.name)
with tarfile.open(out/f'vlno-sdk-{version}-source.tar.gz','w:gz') as archive:
    for prefix in ['README.md','CHANGELOG.md','NOTICE','AGENTS.md','.gitignore','.env.example','docs','examples','scripts','python','cli']:
        path=root/prefix
        files=[path] if path.is_file() else sorted(path.rglob('*'))
        for file in files:
            if not file.is_file():continue
            relative=file.relative_to(root)
            if any(part in {'build','dist','__pycache__'} or part.endswith('.egg-info') for part in relative.parts):continue
            if file.suffix=='.pyc':continue
            archive.add(file,arcname=f'vlno-sdk-{version}/'+str(relative))
assets=sorted(p for p in out.iterdir() if p.is_file() and p.name!='SHA256SUMS')
(out/'SHA256SUMS').write_text(''.join(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+p.name+'\n' for p in assets))
print('Built',len(assets),'release assets')
