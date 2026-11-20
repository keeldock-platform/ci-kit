import base64
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

SPEC = importlib.util.spec_from_file_location('public_content', Path(__file__).parents[1] / 'policy/public_content.py')
scan = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(scan)

class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.parent = Path(self.tmp.name)
        self.root = self.parent / 'repo'
        self.root.mkdir()
        self.policy = self.parent / 'policy.json'
        self.policy.write_text(json.dumps({'schema_version': 1, 'terms': ['example-confidential'], 'markers': ['FAKE_MARKER_123']}))
        self.git('init', '-q')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.git('config', 'user.name', 'Fixture')
        self.git('config', 'commit.gpgsign', 'false')
        self.write('README.md', 'generic tooling')
        self.commit('initial')
        self.base = self.git('rev-parse', 'HEAD').decode().strip()
    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.root), *args], stderr=subprocess.DEVNULL)
    def write(self, name, text):
        p = self.root / name
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(text)
    def commit(self, msg):
        self.git('add', '.')
        self.git('commit', '-qm', msg)
    def findings(self):
        return scan.scan(self.root, self.policy, self.base)
    def test_generic_tree_passes(self):
        self.assertEqual([], self.findings())
    def test_missing_or_empty_private_policy_refuses(self):
        self.policy.write_text('{"schema_version":1,"terms":[],"markers":[]}')
        with self.assertRaises(ValueError): self.findings()
    def test_policy_in_public_tree_refuses(self):
        inside = self.root / 'policy.json'
        inside.write_bytes(self.policy.read_bytes())
        with self.assertRaises(ValueError): scan.scan(self.root, inside)
    def test_readme_and_workflow_titles_refuse(self):
        for name in ['README.md', '.github/workflows/check.yml']:
            with self.subTest(name=name):
                self.write(name, 'name: EXAMPLE-CONFIDENTIAL')
                self.git('add', '.')
                self.assertTrue(self.findings())
                self.write(name, 'generic')
                self.git('add', '.')
    def test_filename_and_symlink_target_refuse(self):
        self.write('example-confidential.txt', 'generic')
        self.git('add', '.')
        self.assertTrue(self.findings())
        self.git('rm', '-qf', 'example-confidential.txt')
        (self.root / 'link').symlink_to('example-confidential')
        self.git('add', '.')
        self.assertTrue(self.findings())
    def test_intermediate_commit_and_message_cannot_be_hidden(self):
        self.write('README.md', 'FAKE_MARKER_123')
        self.commit('candidate')
        self.write('README.md', 'generic')
        self.commit('remove marker')
        self.assertTrue(self.findings())
    def test_commit_message_and_ref_refuse(self):
        self.git('commit', '--allow-empty', '-qm', 'example-confidential')
        self.assertTrue(self.findings())
        self.assertTrue(scan.scan(self.root, self.policy, self.base, ref='refs/heads/example-confidential'))
    def test_case_unicode_and_encoded_canaries(self):
        examples = ['FAKE_MARKER_123', 'ＦＡＫＥ＿ＭＡＲＫＥＲ＿１２３', '%46%41%4b%45%5f%4d%41%52%4b%45%52%5f%31%32%33', '&#70;AKE_MARKER_123', r'\u0046AKE_MARKER_123', base64.b64encode(b'FAKE_MARKER_123').decode()]
        for text in examples:
            with self.subTest(text=text):
                self.write('README.md', text)
                self.git('add', '.')
                self.assertTrue(self.findings())
    def test_utf16_and_binary_fail_closed(self):
        for data in ['FAKE_MARKER_123'.encode('utf-16'), b'\xff\x00\x81']:
            with self.subTest(data=data):
                (self.root / 'README.md').write_bytes(data)
                self.git('add', '.')
                self.assertTrue(self.findings())
    def test_dirty_and_index_versions_both_scanned(self):
        self.write('README.md', 'FAKE_MARKER_123')
        self.git('add', '.')
        self.write('README.md', 'generic')
        self.assertTrue(self.findings())
    def test_identical_objects_are_inspected_once_without_losing_locations(self):
        for name in ('first.txt', 'second.txt'):
            self.write(name, 'FAKE_MARKER_123')
        self.commit('add repeated object')
        self.git('commit', '--allow-empty', '-qm', 'second revision')
        oid = self.git('rev-parse', 'HEAD:first.txt').decode().strip()
        original = scan.git
        with mock.patch.object(scan, 'git', wraps=original) as calls:
            hits = self.findings()
        reads = [c for c in calls.call_args_list if c.args[1:] == ('cat-file', 'blob', oid)]
        self.assertEqual(1, len(reads))
        labels = {hit['location'] for hit in hits}
        for name in ('first.txt', 'second.txt'):
            self.assertIn('index:' + name, labels)
            self.assertIn('worktree:' + name, labels)
            self.assertEqual(2, sum(label.startswith('history:') and label.endswith(':' + name) for label in labels))

    def test_oversized_immutable_blob_is_rejected_before_content_read(self):
        self.write('oversized.txt', 'a' * 4096)
        self.commit('oversized fixture')
        oid = self.git('rev-parse', 'HEAD:oversized.txt').decode().strip()
        original = scan.git
        with mock.patch.object(scan, 'MAX_FILE', 1024), mock.patch.object(scan, 'git', wraps=original) as calls:
            hits = self.findings()
        self.assertFalse(any(c.args[1:] == ('cat-file', 'blob', oid) for c in calls.call_args_list))
        self.assertTrue(any(h['location'] == 'index:oversized.txt' and h['reason'] == 'size_limit' for h in hits))
        self.assertTrue(any(h['location'].endswith(':oversized.txt') and h['location'].startswith('history:') and h['reason'] == 'size_limit' for h in hits))

    def test_unknown_major_duplicate_keys_refuse(self):
        for body in ['{"schema_version":2,"terms":["abc"],"markers":["def"]}', '{"schema_version":1,"schema_version":1,"terms":["abc"],"markers":["def"]}']:
            self.policy.write_text(body)
            with self.assertRaises(ValueError): self.findings()


    def test_root_history_without_base_cannot_hide_intermediate_content(self):
        self.write('README.md','FAKE_MARKER_123'); self.commit('add fixture')
        self.write('README.md','generic'); self.commit('remove fixture')
        self.assertTrue(scan.scan(self.root,self.policy))
    def test_other_publication_refs_and_annotated_tag_metadata_scanned(self):
        self.git('tag','-a','safe-label','-m','FAKE_MARKER_123')
        self.assertTrue(scan.scan(self.root,self.policy,all_refs=True))
