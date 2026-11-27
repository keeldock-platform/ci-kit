"""Only the source-free Go ownership tests invoke these fixed synthetic cases."""
import importlib.util,json,os,sys
from pathlib import Path
SPEC=importlib.util.spec_from_file_location('owner_fixture_stage',Path(__file__).with_name('stage.py'))
stage=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(stage)
mode=sys.argv[1];work=Path(sys.argv[2])
if mode=='timeout' or mode=='cancel':
    child='import os,time;open("child.pid","w").write(str(os.getpid()));time.sleep(60)'
elif mode=='descendant-overflow':
    child='import os,subprocess,sys,time\np=subprocess.Popen([sys.executable,"-I","-B","-c","import time;time.sleep(60)"])\nopen("child.pid","w").write(str(p.pid))\nfor i in range(2048):os.write(2,b"x"*8192)'
else:raise SystemExit(3)
try:stage.command([sys.executable,'-I','-B','-c',child],work)
except stage.CaptureRefused as error:
    # Test-only PID source for owner assertions, no production record authority.
    os.write(2,(json.dumps(error.record)+'\n').encode());raise SystemExit(6)
