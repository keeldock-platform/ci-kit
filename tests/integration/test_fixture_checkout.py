import json
from pathlib import Path
import subprocess
import tempfile
import selectors
import time
import unittest

ROOT=Path(__file__).resolve().parents[2]
class FixtureCheckoutTests(unittest.TestCase):
 def test_declared_commands_work_after_git_checkout(self):
  with tempfile.TemporaryDirectory() as d:
   source=Path(d)/'source'; checkout=Path(d)/'checkout'
   def run(args,cwd=ROOT):
    return subprocess.run(args,cwd=cwd,check=True,capture_output=True)
   run(['node','fixtures/synthetic/materialize.mjs',str(source)])
   run(['git','init','-b','main'],source);run(['git','add','.'],source)
   run(['git','-c','user.name=Fixture Maintainer','-c','user.email=fixture@example.invalid','-c','commit.gpgsign=false','commit','-m','Add fixture'],source)
   run(['git','clone','--no-hardlinks',str(source),str(checkout)])
   cfg=json.loads((checkout/'.kd-ci.json').read_text())
   self.assertEqual('canary',cfg['concern'])
   self.assertIn('rust',cfg['unit_roots'])
   shared=json.loads((checkout/'ci/units.json').read_text())
   self.assertEqual(11,len(shared['packs']))
   for command in shared['packs']['macos-arm64']['setup']:
    run(command['argv'],checkout/command['cwd'])
   for name in ['rust','typescript','plugin-alpha','plugin-beta']:
    check=json.loads((checkout/name/'ci-unit.json').read_text())['checks']['unit']
    for report in check['reports']:
     self.assertTrue((checkout/report).parent.is_dir())
    for command in check['commands']['default']:
     run(command['argv'],checkout/command['cwd'])
    self.assertIn('<testcase', (checkout/check['reports'][0]).read_text())

 def test_canary_surfaces_and_original_failure_codes(self):
  with tempfile.TemporaryDirectory() as d:
   source=Path(d)/'source'
   subprocess.run(['node','fixtures/synthetic/materialize.mjs',str(source)],cwd=ROOT,check=True,capture_output=True)
   (source/'private-test-content.txt').write_text('fixture-private-marker-9842')
   for scenario in ['success','failure','stdout','stderr','ansi','annotation','crash','timeout','cancel']:
    (source/'ci/scenario.json').write_text(json.dumps({'scenario':scenario}))
    p=subprocess.Popen(['node','ci/canary.mjs'],cwd=source,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
    prefix_out=b'';prefix_err=b''
    if scenario in ['timeout','cancel']:
     selector=selectors.DefaultSelector()
     selector.register(p.stdout,selectors.EVENT_READ);selector.register(p.stderr,selectors.EVENT_READ)
     deadline=time.monotonic()+10
     while b'\x1b[31m' not in prefix_out or b'::error::' not in prefix_err:
      if time.monotonic()>deadline:
       p.kill();p.communicate();self.fail('canary did not reach captured output stage')
      for key,_ in selector.select(timeout=0.5):
       chunk=key.fileobj.read1(4096)
       if key.fileobj is p.stdout:prefix_out+=chunk
       else:prefix_err+=chunk
     selector.close()
     self.assertIsNone(p.poll())
     p.kill() if scenario=='timeout' else p.terminate()
    stdout,stderr=p.communicate(timeout=10)
    stdout=prefix_out+stdout;stderr=prefix_err+stderr
    self.assertIn(b'fixture-private-marker-9842',stdout)
    self.assertIn(b'fixture-private-marker-9842',stderr)
    self.assertIn(b'\x1b[31m',stdout);self.assertIn(b'::error::',stderr)
    if scenario=='failure':self.assertEqual(23,p.returncode)
    elif scenario in ['crash','timeout','cancel']:self.assertNotEqual(0,p.returncode)
    else:self.assertEqual(0,p.returncode)
