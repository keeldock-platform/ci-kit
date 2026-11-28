"""Real source-free capture/archive fixtures; AUTHORED / NOT RUN.

The hung/descendant/timeout cases run only through the Go ownership fixture.
These direct calls use finite children; no Node/npm, package, verifier or network.
"""
import gzip,importlib.util,io,json,os,struct,sys,tarfile,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
SPEC=importlib.util.spec_from_file_location('bounded_lock_stage',Path(__file__).with_name('stage.py'))
stage=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(stage)
def header(name,size,kind=tarfile.REGTYPE):
    item=tarfile.TarInfo(name);item.size=size;item.type=kind
    return item.tobuf(format=tarfile.GNU_FORMAT)
class CaptureBounds(unittest.TestCase):
    def test_actual_spawn_error_identity(self):
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaises(stage.CaptureRefused) as raised:stage.command([str(Path(root)/'missing-executable')],Path(root))
            self.assertEqual(raised.exception.record['error'],2)
            self.assertFalse(raised.exception.record['started'])
    def test_actual_noisy_stdout_and_stderr_stop_with_bounded_buffers(self):
        for fd in [1,2]:
            with self.subTest(fd=fd),tempfile.TemporaryDirectory() as root:
                code='import os\nfor i in range(2048): os.write('+str(fd)+',b"x"*8192)'
                with self.assertRaises(stage.CaptureRefused) as raised:stage.command([sys.executable,'-I','-B','-c',code],Path(root))
                record=raised.exception.record
                self.assertEqual(record['overflow_stream'],'stdout' if fd==1 else 'stderr')
                self.assertLessEqual(record['stdout_bytes'],65536);self.assertLessEqual(record['stderr_bytes'],65536)
                self.assertTrue(record['child_exit_observed']);self.assertTrue(record['outer_cleanup_required'])
                self.assertIsNotNone(record['exit_code'])
                self.assertEqual(list(Path(root).iterdir()),[]) # no growing output files
    def test_original_exit_and_signal_identity(self):
        for code,want in [('raise SystemExit(7)',7),('import os,signal;os.kill(os.getpid(),signal.SIGTERM)',-15)]:
            with self.subTest(code=code),tempfile.TemporaryDirectory() as root:
                with self.assertRaises(stage.CaptureRefused) as raised:stage.command([sys.executable,'-I','-B','-c',code],Path(root))
                self.assertEqual(raised.exception.record['exit_code'],want)
                self.assertIsNone(raised.exception.record['overflow_stream'])
    def test_exact_stream_limit_control(self):
        with tempfile.TemporaryDirectory() as root:
            raw=stage.command([sys.executable,'-I','-B','-c','import os;os.write(1,b"x"*65536)'],Path(root))
            self.assertEqual(len(raw),65536)
class ArchiveBounds(unittest.TestCase):
    def test_expansion_is_bounded_before_tarfile_open(self):
        raw=gzip.compress(b'\0'*8192)
        with tempfile.TemporaryDirectory() as root,patch.object(stage.tarfile,'open',side_effect=AssertionError('parser before budget')):
            path=Path(root)/'bounded.tar'
            with self.assertRaises(stage.Refused):stage.bounded_tar(raw,path,budget=1024)
            self.assertLessEqual(path.stat().st_size,1024)
    def test_oversized_pax_and_gnu_headers_refuse_before_parser(self):
        for kind in [tarfile.XHDTYPE,tarfile.XGLTYPE,tarfile.GNUTYPE_LONGNAME,tarfile.GNUTYPE_LONGLINK]:
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as root:
                # Tiny physical body with huge declaration cannot request memory.
                raw=gzip.compress(header('metadata',1<<40,kind)+bytes(1024))
                with self.assertRaises(stage.Refused):stage.bounded_tar(raw,Path(root)/'bad.tar')
    def test_metadata_and_padding_are_in_total_budget(self):
        raw=gzip.compress(header('metadata',4096,tarfile.XHDTYPE)+b'x'*4096+bytes(1024))
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaises(stage.Refused):stage.bounded_tar(raw,Path(root)/'bad.tar',budget=2048)
    def test_truncated_extended_header_and_gzip_refuse(self):
        fixtures=[gzip.compress(header('metadata',4096,tarfile.GNUTYPE_LONGNAME)+bytes(1024)),gzip.compress(bytes(2048))[:-5]]
        for raw in fixtures:
            with tempfile.TemporaryDirectory() as root,self.assertRaises((stage.Refused,EOFError,OSError,tarfile.HeaderError)):
                stage.bounded_tar(raw,Path(root)/'bad.tar')
    def test_metadata_aggregate_and_count_refuse_before_yield(self):
        for count,size in [(129,1),(5,1<<20)]:
            body=b''.join(header('metadata',size,tarfile.XHDTYPE)+b'x'*size+bytes((-size)%512) for _ in range(count))+bytes(1024)
            with self.subTest(count=count),tempfile.TemporaryDirectory() as root,self.assertRaises(stage.Refused):stage.bounded_tar(gzip.compress(body),Path(root)/'bad.tar')
    def test_bounded_reader_rejects_declared_large_read_and_seek(self):
        reader=stage.BoundedTarReader(io.BytesIO(bytes(4096)),4096)
        with self.assertRaises(stage.Refused):reader.read(1<<40)
        with self.assertRaises(stage.Refused):reader.seek(1<<40)
    def test_complete_npm_control_with_valid_extended_metadata(self):
        files={'package/package.json':b'{"name":"npm","version":"12.2.2"}','package/bin/npm-cli.js':b'never executed'}
        raw=io.BytesIO()
        with tarfile.open(fileobj=raw,mode='w:gz',format=tarfile.PAX_FORMAT) as archive:
            for name,content in files.items():
                item=tarfile.TarInfo(name);item.size=len(content);item.pax_headers={'comment':'bounded harmless metadata'};archive.addfile(item,io.BytesIO(content))
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);work=root/'work';dest=root/'dest';work.mkdir();dest.mkdir()
            stage.extract_npm(raw.getvalue(),{'file_count':2,'unpacked_size':sum(map(len,files.values()))},dest,work,'12.2.2')
            self.assertEqual(set(stage.tree(dest/'lib/node_modules/npm')),{'package.json','bin/npm-cli.js'})
if __name__=='__main__':unittest.main()
