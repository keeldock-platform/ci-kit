"""Enforce the boundary between public bootstrap and source-bearing execution."""
from pathlib import Path
import re
import unittest
import importlib.util
spec=importlib.util.spec_from_file_location("diagnostics_policy",Path(__file__).resolve().parents[1]/"policy/diagnostics_policy.py")
policy=importlib.util.module_from_spec(spec);spec.loader.exec_module(policy)

ROOT=Path(__file__).resolve().parents[2]
class DiagnosticsPolicy(unittest.TestCase):
    def test_public_set_is_source_free(self):
        # This is the complete production public diagnostic set. It has a fixed
        # public repository checkout and no source map/token/request command.
        public=(ROOT/'.github/actions/kit-bootstrap/action.yml').read_text()
        executable=public[public.index('    - name: Public control'):]
        self.assertIn('repository: keeldock-platform/ci-kit',executable)
        self.assertIn('PUBLIC_TOOL_FAILURE',executable)
        self.assertTrue(policy.source_free_bootstrap(public))
        self.assertFalse(policy.source_free_bootstrap(public+'\n        cat private-checkout/source.rs\n'))
        self.assertFalse(policy.source_free_bootstrap(public.replace('npm ci --prefix planner','npm ci --prefix private-checkout')))
        for forbidden in ('SOURCE_READ','SOURCE_MAP','CI_KIT_OPERATION','ci-private-', 'ci-diagnostics','./cmd/kd-ci'):
            self.assertNotIn(forbidden,executable)
        self.assertEqual(re.findall(r'repository: (.+)',executable),['keeldock-platform/ci-kit'])
        # Every production private step keeps the default captured process path.
        for name in ('runtime.go','pack.go','android.go'):
            source=(ROOT/'cmd/ci-kit'/name).read_text()
            self.assertNotIn('PUBLIC_TOOL_FAILURE',source)
            self.assertNotIn('PUBLIC_VERIFY',source)
        public_test=(ROOT/'internal/github/registered_smoke_test.go').read_text()
        self.assertIn('CI_REGISTERED_SIGNATURE_SMOKE',public_test)
        self.assertIn('"clone", "--no-hardlinks", "--no-checkout", root, fixture',public_test)
        self.assertNotIn('SOURCE_READ',public_test)
        self.assertNotIn('SOURCE_MAP',public_test)
        self.assertNotIn('trace2.eventTarget=1',public_test)
        self.assertIn('"GIT_TRACE2_EVENT": "1"',public_test)
        # Test oracle proves a source-reading addition is refused.
        malicious=executable+'\nSOURCE_READ_01=secret\n'
        self.assertTrue(any(s in malicious for s in ('SOURCE_READ','SOURCE_MAP')))

    def test_only_ciphertext_uploaded(self):
        files=list((ROOT/'.github/workflows').glob('pack-*.yml'))+[ROOT/'.github/workflows/validate.yml']
        self.assertEqual(len(files),12)
        for path in files:
            text=path.read_text()
            if path.name.startswith("pack-") and "if: github.repository == ''" in text:
                self.assertNotIn('SOURCE_READ',text)
                self.assertNotIn('upload-artifact',text)
                continue
            self.assertIn('name: Encrypted failure diagnostics',text)
            block=text.split('name: Encrypted failure diagnostics',1)[1]
            self.assertIn("outputs.diagnostics == 'ready'",block)
            self.assertIn('retention-days: 1',block)
            self.assertIn('actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9',block)
            upload_path=re.search(r'path: (.+)',block)[1]
            self.assertRegex(upload_path,r'^\$\{\{ runner.temp \}\}/ci-diagnostics/failure-[a-z0-9-]+-\$\{\{ github.run_attempt \}\}\.age$')
            self.assertNotIn('*',upload_path)

if __name__=='__main__':unittest.main()
