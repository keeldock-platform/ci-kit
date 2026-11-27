"""Small inert defensive entry-point fixtures, AUTHORED / NOT RUN.

No native/executable content, large schedules, external process or network.
Test-local small budgets exercise boundaries without oversized archives.
"""
import gzip,hashlib,importlib.util,io,tarfile,tempfile,types,unittest
from pathlib import Path
from unittest.mock import patch
SPEC=importlib.util.spec_from_file_location('defensive_metadata_stage',Path(__file__).with_name('stage.py'))
stage=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(stage)
PACKAGE=b'{"name":"npm","version":"12.2.2"}'
FILES={'package/package.json':PACKAGE,'package/bin/npm-cli.js':b'inert text only'}
def header(name,size,kind=tarfile.REGTYPE):
    info=tarfile.TarInfo(name);info.size=size;info.type=kind
    return info.tobuf(format=tarfile.GNU_FORMAT)
def frame(name,body,kind=tarfile.REGTYPE):
    return header(name,len(body),kind)+body+bytes((-len(body))%512)
def pax(key,value):
    content=key.encode()+b'='+value+b'\n';length=len(content)+2
    for _ in range(10):
        result=str(length).encode()+b' '+content
        if len(result)==length:return result
        length=len(result)
    raise AssertionError('fixture length')
def sized_comment(size):
    prefix=str(size).encode()+b' comment='
    require_length=size-len(prefix)-1
    assert require_length>=0
    return prefix+b'x'*require_length+b'\n'
def complete(metadata=b'',end=bytes(1024)):
    return gzip.compress(metadata+b''.join(frame(k,v) for k,v in FILES.items())+end)
def entry(role,raw,work,dest):
    if role=='npm':stage.extract_npm(raw,{'file_count':2,'unpacked_size':sum(map(len,FILES.values()))},dest,work,'12.2.2')
    else:stage.extract_node(raw,'expected-node-never-executed',dest,work)
class DefensiveMetadata(unittest.TestCase):
    def refuses_before_parser(self,role,raw):
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);work=root/'work';dest=root/'dest';work.mkdir();dest.mkdir()
            with patch.object(stage.tarfile,'open',side_effect=AssertionError('unvalidated metadata reached parser')) as parser:
                with self.assertRaises(stage.Refused):entry(role,raw,work,dest)
                parser.assert_not_called()
            self.assertEqual(list(dest.rglob('*')),[])
    def test_unsupported_pax_fields_refuse_at_both_entries(self):
        for role in ['npm','node']:
            for key in ['size','GNU.sparse.map','GNU.sparse.size','GNU.sparse.major','SCHILY.realsize','SCHILY.filetype','hdrcharset']:
                with self.subTest(role=role,key=key):self.refuses_before_parser(role,complete(frame('pax',pax(key,b'0'),tarfile.XHDTYPE)))
    def test_sparse_unknown_and_nondata_bodies_refuse(self):
        for role in ['npm','node']:
            for kind in [tarfile.GNUTYPE_SPARSE,tarfile.CONTTYPE,b'V']:
                with self.subTest(role=role,kind=kind):self.refuses_before_parser(role,complete(header('unsupported',0,kind)))
            for kind in [tarfile.DIRTYPE,tarfile.SYMTYPE,tarfile.LNKTYPE]:
                with self.subTest(role=role,kind=kind):self.refuses_before_parser(role,complete(frame('nondata',b'x',kind)))
    def test_harmless_complete_npm_actual_extraction(self):
        for kind in [tarfile.XHDTYPE,tarfile.XGLTYPE]:
            raw=complete(frame('pax',pax('comment',b'harmless'),kind))
            with self.subTest(kind=kind),tempfile.TemporaryDirectory() as root:
                root=Path(root);work=root/'work';dest=root/'dest';work.mkdir();dest.mkdir()
                entry('npm',raw,work,dest)
                expected={k[len('package/'):]:hashlib.sha256(v).hexdigest() for k,v in FILES.items()}
                self.assertEqual(stage.tree(dest/'lib/node_modules/npm'),expected)
    def test_small_local_metadata_boundary_and_overflow(self):
        # One tar block boundary, never a large metadata body or expansion input.
        with patch.object(stage,'MAX_METADATA_BYTES',512):
            for role in ['npm','node']:
                with self.subTest(role=role):self.refuses_before_parser(role,complete(frame('pax',sized_comment(513),tarfile.XHDTYPE)))
            raw=complete(frame('pax',sized_comment(512),tarfile.XHDTYPE))
            with tempfile.TemporaryDirectory() as root:
                root=Path(root);work=root/'work';dest=root/'dest';work.mkdir();dest.mkdir()
                entry('npm',raw,work,dest)
                self.assertEqual((dest/'lib/node_modules/npm/bin/npm-cli.js').read_bytes(),FILES['package/bin/npm-cli.js'])
    def test_small_local_count_aggregate_and_expansion_limits(self):
        record=pax('comment',b'x');one=frame('pax',record,tarfile.XHDTYPE)
        for role in ['npm','node']:
            with self.subTest(role=role),patch.object(stage,'MAX_METADATA_HEADERS',2):self.refuses_before_parser(role,complete(one*3))
            with self.subTest(role=role),patch.object(stage,'MAX_METADATA_TOTAL',len(record)*2-1):self.refuses_before_parser(role,complete(one*2))
            with self.subTest(role=role),patch.object(stage,'MAX_TAR_BYTES',1024):self.refuses_before_parser(role,complete())
    def test_padding_and_end_blocks_refuse_before_parser(self):
        record=pax('comment',b'valid')
        invalid_padding=header('pax',len(record),tarfile.XHDTYPE)+record+b'x'+bytes(511-len(record))
        for role in ['npm','node']:
            with self.subTest(role=role):self.refuses_before_parser(role,complete(invalid_padding))
            for end in [b'',bytes(512),bytes(1024)+b'x'+bytes(511)]:
                with self.subTest(role=role,end_len=len(end)):self.refuses_before_parser(role,complete(end=end))
    def test_small_reader_and_traversal_contract_refusals(self):
        reader=stage.BoundedTarReader(io.BytesIO(bytes(1024)),1024)
        with self.assertRaises(stage.Refused):reader.read()
        with self.assertRaises(stage.Refused):reader.read(-1)
        with self.assertRaises(stage.Refused):reader.read(1025)
        with self.assertRaises(stage.Refused):reader.seek(-1)
        with self.assertRaises(stage.Refused):reader.seek(0,3)
        self.assertEqual(reader.read(512),bytes(512))
        member=types.SimpleNamespace(name='inert',size=1,type=tarfile.REGTYPE,offset_data=512)
        with self.assertRaises(stage.Refused):list(stage.members([member],{512:(0,tarfile.REGTYPE)}))
        with self.assertRaises(stage.Refused):list(stage.members([],{512:(0,tarfile.REGTYPE)}))
if __name__=='__main__':unittest.main()
