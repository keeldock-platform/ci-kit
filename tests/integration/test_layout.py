import importlib.util
from pathlib import Path
import tempfile
import unittest
SPEC=importlib.util.spec_from_file_location('layout',Path(__file__).parents[1]/'policy/layout.py')
layout=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(layout)
class LayoutTests(unittest.TestCase):
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
  self.root=Path(self.temp.name);(self.root/'.github').mkdir()
  (self.root/'.github/CODEOWNERS').write_text('* @example/maintainers\n')
  self.paths={'.github/CODEOWNERS','.github/README.md','README.md','tests/README.md','tests/integration/README.md','tests/integration/check.py'}
 def test_valid_tree(self): self.assertEqual([],layout.check(self.root,self.paths,set()))
 def test_misplaced(self): self.assertIn('misplaced_file',layout.check(self.root,self.paths|{'other.txt'},set()))
 def test_missing_readme(self): self.assertIn('missing_readme',layout.check(self.root,self.paths-{'tests/README.md'},set()))
 def test_public_manifest(self): self.assertIn('public_unit_manifest',layout.check(self.root,self.paths|{'docs/ci-unit.json'},set()))
 def test_legacy_growth(self): self.assertIn('new_legacy_file',layout.check(self.root,self.paths|{'kit/new.py'},set()))
 def test_unknown_workflow(self): self.assertIn('workflow_name',layout.check(self.root,self.paths|{'.github/workflows/custom.yml'},set()))
 def test_missing_owner(self):
  (self.root/'.github/CODEOWNERS').write_text('# no owner\n')
  self.assertIn('ownership_unconfigured',layout.check(self.root,self.paths,set()))

 def test_caller_cannot_copy_implementation(self):
  self.assertIn('caller_implementation_copy',layout.check(self.root,self.paths|{'internal/engine.go'},set(),role='caller'))
