import json
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[2]
class PublicationHookTests(unittest.TestCase):
 def test_first_clean_publication_has_no_origin_main(self):
  with tempfile.TemporaryDirectory() as d:
   root=Path(d)/'repo';root.mkdir();policy=Path(d)/'policy.json'
   policy.write_text(json.dumps({'schema_version':1,'terms':['fake-private-term'],'markers':['fake-private-marker']}))
   for name in ['README.md','tests/README.md','tests/policy/README.md','.github/README.md']:
    p=root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text('Generic tooling\n')
   for name in ['layout.py','public_content.py','pre-push']:shutil.copyfile(ROOT/'tests/policy'/name,root/'tests/policy'/name)
   (root/'.ci-layout.json').write_text('{"schema_version":1,"role":"kit"}\n')
   (root/'.github/CODEOWNERS').write_text('* @example\n')
   def git(*args):return subprocess.check_output(['git','-C',str(root),*args],stderr=subprocess.DEVNULL).decode().strip()
   git('init','-b','main');git('config','user.name','Fixture');git('config','user.email','fixture@example.invalid');git('config','commit.gpgsign','false');git('add','.');git('commit','-m','Add fixture')
   sha=git('rev-parse','HEAD');env=dict(os.environ,KD_CI_PUBLIC_DENYLIST=str(policy),KD_CI_PUBLIC_TARGET='https://example.invalid/public-kit.git')
   def hook():return subprocess.run(['/bin/sh',str(root/'tests/policy/pre-push'),'origin',env['KD_CI_PUBLIC_TARGET']],input=f'refs/heads/main {sha} refs/heads/main '+('0'*40)+'\n',text=True,cwd=root,env=env,capture_output=True)
   self.assertEqual(0,hook().returncode)
   (root/'README.md').write_text('fake-private-marker');git('add','.');git('commit','-m','Add marker');(root/'README.md').write_text('Generic tooling');git('add','.');git('commit','-m','Remove marker');sha=git('rev-parse','HEAD')
   self.assertNotEqual(0,hook().returncode)
