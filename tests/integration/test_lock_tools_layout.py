"""Maintained layout positive/refusal fixtures; AUTHORED, NOT RUN."""
import importlib.util
from pathlib import Path
import tempfile,unittest
SPEC=importlib.util.spec_from_file_location('lock_tools_layout',Path(__file__).parents[1]/'policy/layout.py')
layout=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(layout)
class LockToolsLayout(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)
        self.paths={'README.md','.github/README.md','.github/actions/README.md','.github/actions/lock-source-tools/README.md','.github/actions/lock-source-tools/action.yml','packs/README.md','packs/linux-debian/README.md','packs/linux-debian/lock-source-tools/README.md','packs/linux-debian/lock-source-tools/stage.py','packs/linux-debian/lock-source-tools/release-lock.json','packs/linux-debian/lock-source-tools/qualified.json','.github/source-output.json'}
    def check(self,paths=None,role='kit'):
        return layout.check(self.root,self.paths if paths is None else paths,set(),owner_text='* @example/fixture-only\n',role=role)
    def test_exact_platform_helper_action_and_policy(self):self.assertEqual(self.check(),[])
    def test_unrelated_pack_and_old_nonplatform_helper_refuse(self):
        for path in ['packs/lock-source-tools/stage.py','packs/unrelated/helper.py']:
            with self.subTest(path=path):self.assertIn('platform_name',self.check(self.paths|{path}))
    def test_unrelated_github_files_refuse(self):
        for path in ['.github/unrelated.json','.github/source-output.yaml','.github/source-output.json.bak']:
            with self.subTest(path=path):self.assertIn('misplaced_file',self.check(self.paths|{path}))
    def test_policy_is_kit_only(self):self.assertIn('misplaced_file',self.check(role='caller'))
    def test_each_new_readme_remains_required(self):
        for path in ['packs/linux-debian/lock-source-tools/README.md','.github/actions/lock-source-tools/README.md']:
            with self.subTest(path=path):self.assertIn('missing_readme',self.check(self.paths-{path}))
if __name__=='__main__':unittest.main()
