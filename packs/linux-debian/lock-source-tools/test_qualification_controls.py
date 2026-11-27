"""Additional fixed source-free qualification controls, AUTHORED / NOT RUN.

Only future approved-CI tests create ephemeral synthetic EC fixture keys in a
temporary folder. No real key/credential/recipient, payload or network is used.
Actual verifier tests call the maintained native OpenSSL through stage.command;
mocked transport/status checks are separately labelled and are not crypto proof.
"""
import base64,hashlib,importlib.util,tempfile,unittest
from pathlib import Path
from unittest.mock import patch
SPEC=importlib.util.spec_from_file_location('qualification_controls_stage',Path(__file__).with_name('stage.py'))
stage=importlib.util.module_from_spec(SPEC);SPEC.loader.exec_module(stage)
class QualificationControls(unittest.TestCase):
    def synthetic_key(self,work,name):
        secret=work/(name+'.synthetic.pem');public=work/(name+'.public.der')
        stage.command(['/usr/bin/openssl','genpkey','-algorithm','EC','-pkeyopt','ec_paramgen_curve:P-256','-out',str(secret)],work)
        stage.command(['/usr/bin/openssl','pkey','-in',str(secret),'-pubout','-outform','DER','-out',str(public)],work)
        return secret,base64.b64encode(public.read_bytes()).decode()
    def test_real_registry_signature_matching_wrong_key_and_message(self):
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);secret,public=self.synthetic_key(root,'a');_,other=self.synthetic_key(root,'b')
            integrity='sha512-'+base64.b64encode(hashlib.sha512(b'inert synthetic bytes').digest()).decode()
            message=root/'message';message.write_bytes(('npm@12.2.2:'+integrity).encode());signature=root/'signature'
            stage.command(['/usr/bin/openssl','dgst','-sha256','-sign',str(secret),'-out',str(signature),str(message)],root)
            sig=base64.b64encode(signature.read_bytes()).decode()
            for name,key,declared in [('matching',public,integrity),('wrong-key',other,integrity),('wrong-message',public,integrity+'x')]:
                work=root/name;work.mkdir()
                lock={'npm_verification_key':{'expires':None,'scheme':'ecdsa-sha2-nistp256','keytype':'ecdsa-sha2-nistp256','keyid':'synthetic-fixture-only','key':key}}
                entry={'integrity':declared,'signatures':[{'keyid':'synthetic-fixture-only','sig':sig}]}
                with self.subTest(name=name):
                    if name=='matching':stage.verify_npm_signature('12.2.2',entry,lock,work)
                    else:
                        with self.assertRaises(stage.CaptureRefused):stage.verify_npm_signature('12.2.2',entry,lock,work)
    def test_actual_wrong_sri_on_mocked_transport(self):
        class Response:
            status=200;url='https://registry.npmjs.org/npm/-/npm-12.2.2.tgz'
            def __enter__(self):return self
            def __exit__(self,*args):pass
            def read(self,n):return b'inert bytes'
        class Opener:
            def open(self,*args,**kwargs):return Response()
        wrong='sha512-'+base64.b64encode(hashlib.sha512(b'different inert bytes').digest()).decode()
        with patch.object(stage.urllib.request,'build_opener',return_value=Opener()),self.assertRaises(stage.Refused):stage.fetch(Response.url,64,integrity=wrong)
    def test_redirect_and_interruption_refusal_without_network(self):
        with self.assertRaises(stage.Refused):stage.NoRedirect().redirect_request(None,None,302,None,None,None)
        class Interrupted:
            def open(self,*args,**kwargs):raise OSError('synthetic interruption')
        with patch.object(stage.urllib.request,'build_opener',return_value=Interrupted()),self.assertRaises(OSError):stage.fetch('https://registry.npmjs.org/npm/-/npm-12.2.2.tgz',64)
    def test_verifier_status_bad_wrong_expired_revoked_controls(self):
        # Mock status flow, explicitly not OpenPGP cryptographic verification.
        sums=b'a'*64+b'  inert.tar.gz\n';sig=b'inert signature';ring=b'inert public metadata'
        entry={'shasums_url':'sums','shasums_sha256':stage.digest(sums),'signature_url':'signature','signature_sha256':stage.digest(sig),'url':'https://nodejs.org/dist/inert.tar.gz','sha256':'a'*64}
        lock={'public_keyring':{'url':'ring','sha256':stage.digest(ring)},'node_primary_fingerprint':'A'*40}
        values={'sums':sums,'signature':sig,'ring':ring}
        for name,status,want in [('matching','[GNUPG:] VALIDSIG '+'A'*40+'\n',True),('wrong-key','[GNUPG:] VALIDSIG '+'B'*40+'\n',False),('bad','[GNUPG:] BADSIG fixture\n',False),('expired','[GNUPG:] VALIDSIG '+'A'*40+'\n[GNUPG:] EXPKEYSIG fixture\n',False),('revoked','[GNUPG:] VALIDSIG '+'A'*40+'\n[GNUPG:] REVKEYSIG fixture\n',False)]:
            with self.subTest(name=name),tempfile.TemporaryDirectory() as root,patch.object(stage,'fetch',side_effect=lambda url,*a,**k:values[url]),patch.object(stage,'command',return_value=status.encode()):
                if want:stage.verify_node_signature(entry,lock,Path(root))
                else:
                    with self.assertRaises(stage.Refused):stage.verify_node_signature(entry,lock,Path(root))
if __name__=='__main__':unittest.main()
