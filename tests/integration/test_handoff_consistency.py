import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('handoff', Path(__file__).parents[1] / 'policy/handoff_consistency.py')
handoff = importlib.util.module_from_spec(spec)
spec.loader.exec_module(handoff)


class HandoffTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.kit = self.base / 'kit'
        self.callers = self.base / 'caller-templates'
        self.caller = self.callers / 'sample'
        self.action = 'synthetic/setup@' + 'a' * 40
        self.put(self.kit / '.github/workflows/ci-policy.yml', 'jobs:\n  policy:\n    uses: ' + self.action + '\n')
        self.sha = self.commit(self.kit)
        self.ref = handoff.KIT + '.github/workflows/ci-policy.yml@' + self.sha
        self.put(self.caller / '.github/workflows/ci-policy.yml', 'jobs:\n  policy:\n    uses: ' + self.ref + '\n    with:\n      kit_sha: ' + self.sha + '\n')
        self.put(self.caller / '.github/workflows/actions.lock', "version: 'v0.0.3'\n")
        self.commit(self.caller)
        pins = [self.action, self.ref, handoff.KIT + '.github/workflows/validate.yml@' + self.sha]
        values = dict(patterns_allowed=pins, allowed_actions='selected', sha_pinning_required=True,
                      github_owned_allowed=False, verified_allowed=False)
        self.doc = dict(production_sha=self.sha, settings_count=3,
                        reconciliation=dict(reviewed_production_sha=self.sha, inventory_file='inventory.json'),
                        settings=[dict(id='parent', kind='organization_actions_permissions', target='sample', values=values),
                                  dict(id='child', kind='repository_actions_permissions', target='sample/caller', values=dict(values)),
                                  dict(id='contract', kind='caller_workflow_contract', target='sample/caller',
                                       values=dict(kit_sha=self.sha, templates='caller-templates/sample'))])
        self.settings = self.base / 'settings.json'
        self.put(self.base / 'inventory.json', json.dumps(dict(repositories=[])))

    def put(self, path, text):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def commit(self, root):
        def git(*args):
            return subprocess.check_output(['git', '-C', str(root), '-c', 'user.name=Synthetic CI', '-c',
                                            'user.email=ci@example.invalid', '-c', 'commit.gpgsign=false', *args],
                                           stderr=subprocess.DEVNULL, text=True).strip()
        git('init', '-q')
        git('add', '.')
        git('commit', '-qm', 'Synthetic fixture')
        return git('rev-parse', 'HEAD')

    def check(self):
        self.settings.write_text(json.dumps(self.doc))
        return handoff.check(self.kit, self.settings, self.callers)

    def test_consistent_handoff_and_retained_legacy_pin(self):
        for row in self.doc['settings'][:2]:
            row['values']['patterns_allowed'] = [*row['values']['patterns_allowed'], handoff.KIT + '.github/workflows/validate.yml@' + 'b' * 40]
        self.assertEqual('PASS', self.check()['pin_consistency'])

    def test_head_advance_fails_without_regenerating_handoff(self):
        self.put(self.kit / 'README.md', 'Next revision\n')
        self.commit(self.kit)
        with self.assertRaisesRegex(ValueError, 'settings production pin'):
            self.check()

    def test_nested_settings_pin_drift(self):
        self.doc['settings'][2]['values']['kit_sha'] = 'b' * 40
        with self.assertRaisesRegex(ValueError, 'settings active pin'):
            self.check()

    def test_caller_workflow_and_input_drift(self):
        path = self.caller / '.github/workflows/ci-policy.yml'
        for text in ('jobs:\n  policy:\n    uses: ' + self.ref.replace(self.sha, 'b' * 40) + '\n',
                     'jobs:\n  policy:\n    uses: ' + self.ref + '\n    with:\n      kit_sha: ' + 'b' * 40 + '\n'):
            self.put(path, text)
            self.commit(self.caller)
            with self.assertRaisesRegex(ValueError, 'caller .*pin differs'):
                self.check()

    def test_caller_lock_drift(self):
        self.put(self.caller / '.github/workflows/actions.lock', self.ref.replace(self.sha, 'b' * 40))
        self.commit(self.caller)
        with self.assertRaisesRegex(ValueError, 'caller lock pin'):
            self.check()

    def test_missing_action_in_each_policy_fails(self):
        for row in self.doc['settings'][:2]:
            saved = row['values']['patterns_allowed']
            row['values']['patterns_allowed'] = [p for p in saved if p != self.action]
            with self.assertRaisesRegex(ValueError, 'used action missing'):
                self.check()
            row['values']['patterns_allowed'] = saved

    def test_composite_dependencies_and_unknown_yaml_fail_closed(self):
        path = self.kit / '.github/actions/nested/action.yml'
        self.put(path, 'runs:\n  steps:\n    - uses: synthetic/upload@' + 'c' * 40 + '\n')
        self.assertIn('synthetic/upload@' + 'c' * 40, handoff.remote_uses(self.kit, self.sha))
        self.put(path, 'runs: {steps: [{uses: synthetic/upload@' + 'c' * 40 + '}]}\n')
        with self.assertRaisesRegex(ValueError, 'unsupported uses syntax'):
            handoff.remote_uses(self.kit, self.sha)

    def test_missing_parent_legacy_union(self):
        self.doc['settings'][1]['values']['patterns_allowed'] = [*self.doc['settings'][1]['values']['patterns_allowed'], 'synthetic/legacy@' + 'd' * 40]
        with self.assertRaisesRegex(ValueError, 'child pin missing'):
            self.check()


if __name__ == '__main__':
    unittest.main()
