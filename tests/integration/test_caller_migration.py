import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

POLICY=Path(__file__).parents[1]/'policy'
sys.path.insert(0,str(POLICY))
import layout
import immutable_actions
import public_tree_scan
import workflow_lock


class CallerMigrationTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name)
        subprocess.run(['git','init','-q',str(self.root)],check=True)
        subprocess.run(['git','-C',str(self.root),'config','user.name','Fixture'],check=True)
        subprocess.run(['git','-C',str(self.root),'config','user.email','fixture@example.invalid'],check=True)
        subprocess.run(['git','-C',str(self.root),'config','commit.gpgsign','false'],check=True)
        self.write('.ci-layout.json',json.dumps({'schema_version':1,'role':'legacy-caller'}))
        self.sha='a'*40
        for name,workflow in [('source-ci.yml','validate.yml'),('pr-lint.yml','ci-policy.yml')]:
            self.write('.github/workflows/'+name,'jobs:\n  check:\n    uses: keeldock-platform/ci-kit/.github/workflows/'+workflow+'@'+self.sha+'\n    with:\n      kit_sha: '+self.sha+'\n')
        self.commit('initial')
    def write(self,path,content):
        target=self.root/path;target.parent.mkdir(parents=True,exist_ok=True)
        target.write_text(content)
    def stage(self): subprocess.run(['git','-C',str(self.root),'add','-A'],check=True)
    def commit(self,msg):
        self.stage()
        subprocess.run(['git','-C',str(self.root),'commit','-qm',msg],check=True)
    def test_structural_role_admits_preserved_hierarchy_without_fixed_list_or_owner(self):
        paths={'.ci-layout.json','.github/workflows/source-ci.yml','kit/new_helper.py','core-ci/check.py'}
        self.assertEqual([],layout.check(self.root,paths,set(),role='legacy-caller'))
        self.assertIn('new_legacy_file',layout.check(self.root,paths,set(),role='caller'))
        self.assertIn('ownership_unconfigured',layout.check(self.root,paths,set(),role='kit'))
    def test_structural_role_rejects_unrelated_implementation_and_bad_workflow_paths(self):
        for path,reason in [('internal/engine.go','caller_implementation_copy'),('other/helper.py','misplaced_file'),('.github/workflows/nested/test.yml','misplaced_file')]:
            self.assertIn(reason,layout.check(self.root,{path},set(),role='legacy-caller'))
    def test_preserved_workflows_are_scanned_and_current_callers_require_exact_kit(self):
        self.assertTrue(immutable_actions.check(self.root,self.sha))
        self.write('.github/workflows/old-check.yml','jobs:\n  old:\n    uses: actions/checkout@main\n')
        self.assertFalse(immutable_actions.check(self.root,self.sha))
        self.write('.github/workflows/old-check.yml','jobs:\n  old:\n    uses: actions/checkout@'+'b'*40+'\n')
        self.assertTrue(immutable_actions.check(self.root,self.sha))
        self.assertFalse(immutable_actions.check(self.root,'c'*40))
    def test_same_tree_action_metadata_and_transitive_refs_must_be_inspectable(self):
        self.write('.github/workflows/old-check.yml','jobs:\n  old:\n    steps:\n      - uses: $/kit/check\n')
        self.write('kit/check/action.yml','runs:\n  using: composite\n  steps:\n    - uses: example/action@main\n');self.stage()
        self.assertFalse(immutable_actions.check(self.root,self.sha))
        self.write('kit/check/action.yml','runs:\n  using: composite\n  steps:\n    - uses: example/action@'+'b'*40+'\n')
        self.assertTrue(immutable_actions.check(self.root,self.sha))
        (self.root/'kit/check/action.yml').unlink()
        self.assertFalse(immutable_actions.check(self.root,self.sha))
    def test_generic_scanner_inspects_index_dirty_content_and_encoded_credentials(self):
        self.assertEqual([],public_tree_scan.scan(self.root))
        token='gh'+'p_'+'q'*36
        self.write('README.md',token);self.stage();self.write('README.md','generic')
        self.assertTrue(public_tree_scan.scan(self.root))
        self.write('README.md',''.join('%%%02x'%ord(ch) for ch in token));self.stage()
        self.assertTrue(public_tree_scan.scan(self.root))
    def test_generic_scanner_scans_bounded_history_and_refuses_shallow_or_unborn(self):
        token='gh'+'p_'+'z'*36
        self.write('note.txt',token);self.commit('add secret')
        self.write('note.txt','clean');self.commit('remove secret')
        self.assertIn('credential_or_local_path',public_tree_scan.scan(self.root))
        shallow=Path(self.tmp.name)/'shallow'
        subprocess.run(['git','clone','-q','--depth','1','file://'+str(self.root),str(shallow)],check=True)
        self.assertEqual(['shallow_history'],public_tree_scan.scan(shallow))
        unborn=Path(self.tmp.name)/'unborn'
        subprocess.run(['git','init','-q',str(unborn)],check=True)
        self.assertEqual(['missing_history'],public_tree_scan.scan(unborn))
    def test_caller_checkout_fetches_full_history(self):
        checkout=(Path(__file__).parents[2]/'.github/actions/caller-checkout/action.yml').read_text()
        self.assertIn('fetch-depth: 0',checkout)
    def test_binary_qualification_requires_exact_prior_path_and_digest(self):
        self.write('new.dat','generic');(self.root/'new.dat').write_bytes(b'\xff\x00');self.stage()
        self.assertTrue(public_tree_scan.scan(self.root))
    def test_lock_exception_is_specific_to_full_sha_traceability_and_no_other_findings(self):
        warning={'severity':'warning','category':'sha-as-ref','dependency':'example/action@'+'a'*40}
        self.assertTrue(workflow_lock.accepted({'valid':True,'findings':[warning]}))
        for field,value in [('category','ref-moved'),('dependency','example/action@main'),('severity','error')]:
            finding=dict(warning);finding[field]=value
            self.assertFalse(workflow_lock.accepted({'valid':True,'findings':[finding]}))
        self.assertFalse(workflow_lock.accepted({'findings':[]}))
        self.assertFalse(workflow_lock.accepted({'valid':True,'findings':[warning,{'severity':'error','category':'incomplete-lockfile'}]}))
        self.assertFalse(workflow_lock.accepted({'valid':True,'findings':[{'severity':'unknown','category':'valid'}]}))
        self.assertFalse(workflow_lock.accepted([]))
