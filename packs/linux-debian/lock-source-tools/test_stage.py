"""AUTHORED / NOT RUN source-free archive/hash/refusal fixtures.

No real runtime, package lifecycle, external verifier or network is executed.
Actual official cryptographic signature/native runtime qualification is the
separate fixed CI qualification operation, never inferred from these mocks.
"""
import base64,hashlib,importlib.util,io,json,os,struct,tarfile,tempfile,unittest
from pathlib import Path
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('lock_stage',Path(__file__).with_name('stage.py'))
stage=importlib.util.module_from_spec(spec);spec.loader.exec_module(stage)

def archive(entries):
    raw=io.BytesIO()
    with tarfile.open(fileobj=raw,mode='w:gz') as tar:
        for name,content,kind in entries:
            info=tarfile.TarInfo(name);info.type=kind
            if kind==tarfile.REGTYPE:info.size=len(content);tar.addfile(info,io.BytesIO(content))
            else:info.linkname='../outside';tar.addfile(info)
    return raw.getvalue()
class ToolStage(unittest.TestCase):
    def test_complete_npm_tree_and_version(self):
        version='12.2.2'
        files={'package.json':json.dumps({'name':'npm','version':version},separators=(',',':')).encode(),'bin/npm-cli.js':b'synthetic never executed','node_modules/synthetic/package.json':b'{}'}
        payload=archive([('package/'+k,v,tarfile.REGTYPE) for k,v in files.items()])
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);work=root/'work';dest=root/'dest';work.mkdir();dest.mkdir()
            stage.extract_npm(payload,{'file_count':3,'unpacked_size':sum(map(len,files.values()))},dest,work,version)
            actual=stage.tree(dest/'lib/node_modules/npm')
            self.assertEqual(actual,{k:hashlib.sha256(v).hexdigest() for k,v in files.items()})
            self.assertEqual(stage.canonical(actual),json.dumps(actual,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode())
            self.assertEqual(stage.digest(stage.canonical(actual)),'d1ed09651791dff42c49a42d36d08c75bd13939b9728949451d1a61f036110a2')
    def test_npm_missing_count_link_and_traversal_refuse(self):
        for name,kind in [('package/../escape',tarfile.REGTYPE),('package/linked',tarfile.SYMTYPE),('package/hard',tarfile.LNKTYPE),('package/device',tarfile.CHRTYPE)]:
            with self.subTest(name=name),tempfile.TemporaryDirectory() as root:
                root=Path(root);work=root/'work';dest=root/'dest';work.mkdir();dest.mkdir()
                with self.assertRaises(stage.Refused):stage.extract_npm(archive([(name,b'x',kind)]),{'file_count':1,'unpacked_size':1},dest,work,'12.2.2')
                self.assertFalse((root/'escape').exists())
        package=b'{"name":"npm","version":"12.2.2"}'
        raw=archive([('package/package.json',package,tarfile.REGTYPE),('package/bin/npm-cli.js',b'not executed',tarfile.REGTYPE)])
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);work=root/'work';dest=root/'dest';work.mkdir();dest.mkdir()
            with self.assertRaises(stage.Refused):stage.extract_npm(raw,{'file_count':3,'unpacked_size':len(package)+len(b'not executed')},dest,work,'12.2.2')
    def test_native_elf_positive_and_wrong_architecture(self):
        elf=bytearray(64);elf[:5]=b'\x7fELF\x02';elf[5]=1;elf[18:20]=struct.pack('<H',62)
        for arch,want in [(62,True),(183,False)]:
            elf[18:20]=struct.pack('<H',arch)
            with self.subTest(arch=arch),tempfile.TemporaryDirectory() as root:
                root=Path(root);work=root/'work';dest=root/'dest';work.mkdir();dest.mkdir()
                raw=archive([('node-v24.22.2-linux-x64/bin/node',bytes(elf),tarfile.REGTYPE)])
                if want:
                    stage.extract_node(raw,'node-v24.22.2-linux-x64/bin/node',dest,work)
                    self.assertEqual((dest/'bin/node').read_bytes(),bytes(elf))
                else:
                    with self.assertRaises(stage.Refused):stage.extract_node(raw,'node-v24.22.2-linux-x64/bin/node',dest,work)
                    self.assertFalse((dest/'bin/node').exists())
    def test_complete_tree_links_and_unsafe_names_refuse(self):
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);(root/'normal').write_bytes(b'fixture')
            self.assertEqual(stage.tree(root),{'normal':hashlib.sha256(b'fixture').hexdigest()})
            (root/'unsafe name').write_bytes(b'fixture')
            with self.assertRaises(stage.Refused):stage.tree(root)
        for name in ['../escape','/absolute','a/../b','a//b','a\\b','a/<b>']:
            with self.subTest(name=name),self.assertRaises(stage.Refused):stage.safe_name(name)
    def test_hash_and_sri_mismatch_refuse(self):
        class Response:
            status=200
            url='https://registry.npmjs.org/npm/-/npm-12.2.2.tgz'
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def read(self,n):return b'synthetic tar payload'
        class Opener:
            def open(self,*args,**kwargs):return Response()
        correct='sha512-'+base64.b64encode(hashlib.sha512(b'synthetic tar payload').digest()).decode()
        with patch.object(stage.urllib.request,'build_opener',return_value=Opener()):
            self.assertEqual(stage.fetch(Response.url,100,integrity=correct),b'synthetic tar payload')
            with self.assertRaises(stage.Refused):stage.fetch(Response.url,100,sha256='0'*64)
            with self.assertRaises(stage.Refused):stage.fetch(Response.url,1,integrity=correct)
            with self.assertRaises(stage.Refused):stage.fetch('https://untrusted.invalid/npm.tgz',100)
    def test_unapproved_stage_refuses_before_network(self):
        with tempfile.TemporaryDirectory() as parent:
            parent=Path(parent);work=parent/'work';work.mkdir(mode=0o700)
            control=parent/'control';control.mkdir()
            (control/'release-lock.json').write_text(json.dumps({'schema_version':1,'platform':'linux-x64','tools':{'root':{},'runtime':{}}}))
            (control/'qualified.json').write_text(json.dumps({'approved':False}))
            with patch.object(stage,'HERE',control),patch.object(stage.platform,'machine',return_value='x86_64'),patch.object(stage.sys,'platform','linux'),patch.object(stage,'fetch',side_effect=AssertionError('network before qualification')):
                with self.assertRaises(stage.Refused):stage.run('stage',work,parent)
            self.assertFalse((parent/'ci-lock-tools').exists())
if __name__=='__main__':unittest.main()
