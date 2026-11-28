"""Regression tests for bounded, source-free qualification error details."""
import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path

SPEC = importlib.util.spec_from_file_location('qualification_error_stage', Path(__file__).with_name('stage.py'))
stage = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(stage)


class QualificationErrorOutput(unittest.TestCase):
    def test_npm_config_files_are_distinct_empty_private_regular_files(self):
        with tempfile.TemporaryDirectory() as td:
            work=Path(td);npm=work/'npm';(npm/'bin').mkdir(parents=True)
            cli=npm/'bin/npm-cli.js';cli.write_text('')
            user,global_config=stage.npm_config_files(work)
            self.assertNotEqual(user,global_config)
            for path in (user,global_config):
                self.assertTrue(path.is_file());self.assertFalse(path.is_symlink())
                self.assertEqual(path.stat().st_mode & 0o777,0o600);self.assertEqual(path.read_bytes(),b'')
            with self.assertRaises(FileExistsError):stage.npm_config_files(work)

    def test_actual_child_failure_retains_bounded_streams(self):
        with tempfile.TemporaryDirectory() as td:
            work = Path(td)
            child = work / 'child.py'
            child.write_text("import os\nos.write(1,b'useful stdout\\n')\nos.write(2,b'useful stderr\\n')\nraise SystemExit(17)\n")
            with self.assertRaises(stage.CaptureRefused) as caught:
                stage.command(['/usr/bin/python3', '-I', '-B', str(child)], work)
            self.assertIn('useful stdout', caught.exception.stdout.decode())
            self.assertIn('useful stderr', caught.exception.stderr.decode())
            self.assertEqual(caught.exception.record['exit_code'], 17)

    def test_real_child_medium_preamble_reports_head_and_actual_tail(self):
        with tempfile.TemporaryDirectory() as td:
            work=Path(td); child=work/'child.py'
            child.write_text("import os\nos.write(1,b'OUTHEAD'+b'x'*200+b' OUTTAIL')\nos.write(2,b'ERRHEAD'+b'y'*190+b' ACTUAL STDERR ERROR')\nraise SystemExit(17)\n")
            with self.assertRaises(stage.CaptureRefused) as caught:
                stage.command(['/usr/bin/python3','-I','-B',str(child)],work)
            report=json.loads(stage.qualification_failure_output(caught.exception,'qualify'))
            self.assertIn('OUTHEAD',report['child_stdout']);self.assertIn('OUTTAIL',report['child_stdout'])
            self.assertIn('ERRHEAD',report['child_stderr']);self.assertIn('ACTUAL STDERR ERROR',report['child_stderr'])
            self.assertTrue(report['stdout_truncated']);self.assertTrue(report['stderr_truncated'])

    def test_child_allowances_preserve_head_tail_for_medium_large_and_invalid_utf8(self):
        for size in (133,218,640,641):
            with self.subTest(size=size):
                raw=b'HEAD'+b'x'*(size-len(b'HEAD')-len(b'TAIL'))+b'TAIL'
                error=stage.CaptureRefused({'exit_code':1},raw,raw)
                payload=json.loads(stage.qualification_failure_output(error,'qualify'))
                self.assertIn('HEAD',payload['child_stdout']);self.assertIn('TAIL',payload['child_stdout'])
                self.assertIn('HEAD',payload['child_stderr']);self.assertIn('TAIL',payload['child_stderr'])
                self.assertEqual(payload['stdout_truncated'],size>132)
                self.assertEqual(payload['stderr_truncated'],size>132)
        invalid=b'HEAD'+b'\xff'*64+b'TAIL'
        payload=json.loads(stage.qualification_failure_output(stage.CaptureRefused({'exit_code':1},invalid,invalid),'qualify'))
        self.assertIn('HEAD',payload['child_stdout']);self.assertIn('TAIL',payload['child_stdout'])
        self.assertTrue(payload['stdout_truncated'])

    def test_reporting_helper_preserves_actual_child_streams_and_stage_privacy(self):
        error = stage.CaptureRefused({'exit_code': 17, 'started': True}, b'useful out', b'useful child stderr')
        public = stage.qualification_failure_output(error, 'qualify')
        payload = json.loads(public)
        self.assertIn('useful child stderr', payload['child_stderr'])
        self.assertIn('useful out', payload['child_stdout'])
        self.assertLessEqual(len(public), 4096)
        private = stage.qualification_failure_output(error, 'stage')
        self.assertEqual(json.loads(private), {'tool_child_outcome': error.record})
        self.assertNotIn(b'useful', private)

    def test_reporting_helper_keeps_traceback_and_bounds_quote_control_unicode(self):
        try:
            raise RuntimeError('useful qualification detail')
        except RuntimeError as error:
            raw = stage.qualification_failure_output(error, 'qualify-candidate')
        payload = json.loads(raw)
        self.assertEqual(payload['exception_class'], 'RuntimeError')
        self.assertIn('useful qualification detail', payload['exception_message'])
        self.assertFalse(payload['frames_omitted'])
        self.assertLessEqual(len(raw), 4096)
        noisy = stage.CaptureRefused({'exit_code': 2}, stdout=b'MARKER'+b'x'*10000,
                                     stderr=('preamble'*1000+('"\\\x01雪'*1000)+' ACTUAL CHILD TAIL').encode())
        raw = stage.qualification_failure_output(noisy, 'qualify')
        payload = json.loads(raw)
        self.assertLessEqual(len(raw), 4096)
        self.assertTrue(payload['truncated'])
        self.assertIn('MARKER', payload['child_stdout'])
        self.assertIn('ACTUAL CHILD TAIL', payload['child_stderr'])
        self.assertTrue(payload['stderr_truncated'])

    def test_long_traceback_preserves_message_and_bounded_frames(self):
        try:
            def deep(n):
                if n: return deep(n-1)
                raise ValueError('actual long-stack error')
            deep(180)
        except ValueError as error:
            depth=0;tb=error.__traceback__
            while tb is not None: depth+=1;tb=tb.tb_next
            self.assertGreaterEqual(depth,100)
            raw = stage.qualification_failure_output(error, 'qualify')
        payload=json.loads(raw)
        self.assertEqual(payload['exception_class'],'ValueError')
        self.assertEqual(payload['exception_message'],'actual long-stack error')
        self.assertIn('deep',payload['traceback_frames'])
        self.assertTrue(payload['frames_omitted'])
        self.assertTrue(payload['truncated'])
        self.assertLessEqual(len(raw),4096)

    def test_nine_to_thirty_two_frames_mark_retained_frame_omission(self):
        try:
            def medium_deep(n):
                if n: return medium_deep(n-1)
                raise LookupError('medium-stack useful message')
            medium_deep(12)
        except LookupError as error:
            depth=0;tb=error.__traceback__
            while tb is not None: depth+=1;tb=tb.tb_next
            self.assertGreaterEqual(depth,9)
            self.assertLessEqual(depth,32)
            raw=stage.qualification_failure_output(error,'qualify')
        payload=json.loads(raw)
        self.assertEqual(payload['exception_class'],'LookupError')
        self.assertEqual(payload['exception_message'],'medium-stack useful message')
        self.assertIn('medium_deep',payload['traceback_frames'])
        self.assertTrue(payload['frames_omitted'])
        self.assertTrue(payload['truncated'])
        self.assertLessEqual(len(raw),4096)

if __name__ == '__main__':
    unittest.main()
