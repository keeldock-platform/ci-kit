import importlib.util
from pathlib import Path
import unittest
SPEC=importlib.util.spec_from_file_location('workflow_lint',Path(__file__).parents[1]/'policy/workflow_lint.py')
lint=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(lint)
class WorkflowLintProjectionTests(unittest.TestCase):
 def test_native_cache_none_only(self):
  self.assertEqual('name: sample\n\njobs: {}\n',lint.project('name: sample\ncache-mode: none\njobs: {}\n'))
 def test_other_cache_values_remain_visible_to_linter(self):
  for value in ['write','all','none # comment','[none]']:
   text='cache-mode: '+value+'\n'
   self.assertEqual(text,lint.project(text))
 def test_only_native_action_paths_translate(self):
  self.assertEqual('      uses: ./.github/actions/kit-bootstrap\n',lint.project('      uses: $/.github/actions/kit-bootstrap\n', {1}))
 def test_remote_sha_job_refs_commands_unchanged(self):
  text='uses: vendor/action@'+'a'*40+'\nuses: ./.github/workflows/pack.yml\nrun: echo "$/.github/actions/example"\n'
  self.assertEqual(text,lint.project(text))
 def test_line_numbers_preserved(self):
  text='cache-mode: none\n  - uses: $/.github/actions/policy-tools # same revision\n'
  self.assertEqual(text.count('\n'),lint.project(text).count('\n'))
 def test_duplicate_native_cache_refuses(self):
  with self.assertRaises(ValueError): lint.project('cache-mode: none\ncache-mode: none\n')
 def test_shell_block_text_is_not_transformed(self):
  text='run: |\n  uses: $/.github/actions/kit-bootstrap\n'
  self.assertEqual(text,lint.project(text))
if __name__=='__main__':unittest.main()
