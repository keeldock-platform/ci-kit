import importlib.util
from pathlib import Path
import tempfile
import unittest
SPEC=importlib.util.spec_from_file_location('immutable_actions',Path(__file__).parents[1]/'policy/immutable_actions.py')
class ImmutableActionTests(unittest.TestCase):
 def test_mutable_and_foreign_local_refs_refused(self):
  m=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(m)
  for ref,want in [('actions/checkout@'+'a'*40,True),('example/ci/.github/workflows/validate.yml@'+'b'*40,True),('actions/checkout@v7',False),('./evil',False),('$/evil',False),('$/.github/actions/kit-bootstrap',True),('./.github/workflows/validate.yml',True),('example/action@'+'A'*40,False)]:
   with self.subTest(ref=ref):self.assertEqual(want,m.immutable_ref(ref))

 def test_caller_requires_one_reviewed_kit_pin(self):
  import json
  m=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(m)
  with tempfile.TemporaryDirectory() as d:
   root=Path(d);(root/'.github/workflows').mkdir(parents=True)
   (root/'.ci-layout.json').write_text(json.dumps({'schema_version':1,'role':'caller'}))
   sha='a'*40;p=root/'.github/workflows/ci-policy.yml'
   p.write_text('jobs:\n  policy:\n    uses: keeldock-platform/ci-kit/.github/workflows/ci-policy.yml@'+sha+'\n    with:\n      kit_sha: '+sha+'\n')
   self.assertTrue(m.check(root,sha));self.assertFalse(m.check(root,'b'*40));self.assertFalse(m.check(root))
