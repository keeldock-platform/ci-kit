"""Fixed source-free dual-tool qualification/staging. PROPOSED; NOT RUN.

Only the trusted ci-kit tool-stage entry invokes this file under process.Run.
It owns the deadline, bounded outer capture and the complete process group.
No source checkout, install, lifecycle, npx, system mutation or credential exists
in this child. Subcommands inherit that process group; no second cleanup owner.
"""
from pathlib import Path, PurePosixPath
import base64,gzip,hashlib,io,json,os,platform,re,selectors,shutil,struct,subprocess,sys,tarfile,urllib.request

HERE=Path(__file__).resolve().parent
class Refused(Exception):pass
class CaptureRefused(Refused):
    def __init__(self,record):
        super().__init__('tool_capture_refused');self.record=record
def require(value):
    if not value:raise Refused('tool_stage_refused')
def digest(raw):return hashlib.sha256(raw).hexdigest()
def canonical(value):
    # Names are constrained to ASCII without Go's HTML-sensitive characters.
    return json.dumps(value,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()
def safe_name(name):
    require(isinstance(name,str) and re.fullmatch(r'[A-Za-z0-9._@/+~=-]+',name))
    require('//' not in name and not name.startswith('/') and all(p not in ('','..','.') for p in name.rstrip('/').split('/')))
    return name.rstrip('/')
def tree(root):
    result={};total=0
    require(root.is_dir() and not root.is_symlink())
    for directory,dirs,files in os.walk(root,followlinks=False):
        for name in dirs:require(not (Path(directory)/name).is_symlink() and name!='.git')
        for name in files:
            path=Path(directory)/name;require(path.is_file() and not path.is_symlink())
            rel=safe_name(path.relative_to(root).as_posix());require('.git' not in PurePosixPath(rel).parts);size=path.stat().st_size
            total+=size;require(size<=64<<20 and total<=1<<30 and len(result)<100000)
            result[rel]=digest(path.read_bytes())
    return result
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*args,**kwargs):raise Refused('tool_stage_refused')
def fetch(url,limit,sha256=None,integrity=None):
    require(url.startswith(('https://nodejs.org/dist/','https://registry.npmjs.org/npm/-/','https://raw.githubusercontent.com/nodejs/release-keys/481637f813e912c4aa3622d7964ab426c97b8e8d/')))
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),NoRedirect())
    with opener.open(urllib.request.Request(url,headers={'User-Agent':'KeelDock-source-free-tool-stage/1'}),timeout=60) as response:
        require(response.status==200 and response.url==url)
        raw=response.read(limit+1);require(len(raw)<=limit)
    if sha256 is not None:require(digest(raw)==sha256)
    if integrity is not None:
        require(integrity.startswith('sha512-'))
        require(hashlib.sha512(raw).digest()==base64.b64decode(integrity[7:],validate=True))
    return raw
def write(path,raw,executable=False):
    path.parent.mkdir(parents=True,exist_ok=True,mode=0o700)
    with path.open('xb') as stream:require(stream.write(raw)==len(raw))
    path.chmod(0o700 if executable else 0o600)
def command(argv,work):
    # Pipes are drained in bounded chunks, before any file/buffer can grow beyond
    # its declared stream budget. No new session/group is created. The existing
    # Go process.Run owns the deadline and cleanup of this entire process group.
    require(Path(argv[0]).is_absolute())
    env={'PATH':'/usr/bin:/bin','HOME':str(work),'CI':'true','npm_config_offline':'true','npm_config_ignore_scripts':'true','npm_config_update_notifier':'false','PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD':'1'}
    try:child=subprocess.Popen(argv,cwd=work,env=env,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=False)
    except OSError as error:
        raise CaptureRefused({'exit_code':None,'signal':None,'overflow_stream':None,'error':error.errno,'child_exit_observed':False,'stdout_bytes':0,'stderr_bytes':0,'outer_cleanup_required':True,'started':False}) from None
    streams={'stdout':bytearray(),'stderr':bytearray()};overflow=None;error=None
    selector=selectors.DefaultSelector()
    try:
        for name,pipe in [('stdout',child.stdout),('stderr',child.stderr)]:
            os.set_blocking(pipe.fileno(),False);selector.register(pipe,selectors.EVENT_READ,name)
        while selector.get_map() and overflow is None:
            # The trusted outer owner interrupts this wait and every descendant.
            for key,_ in selector.select():
                try:block=os.read(key.fileobj.fileno(),min(8192,65536-len(streams[key.data])+1))
                except BlockingIOError:continue
                if not block:selector.unregister(key.fileobj);key.fileobj.close();continue
                if len(streams[key.data])+len(block)>65536:overflow=key.data;break
                streams[key.data].extend(block)
        if overflow is not None:
            # Stop this producer immediately. This is not a second group cleanup
            # system: Python exits nonzero and Go confirms all descendant cleanup.
            try:child.kill()
            except ProcessLookupError:pass
            try:child.wait(timeout=1)
            except subprocess.TimeoutExpired:pass
        else:
            # Bound the post-EOF reap. A child that closed its pipes and hangs
            # refuses; the outer owner still owns and confirms group cleanup.
            try:child.wait(timeout=1)
            except subprocess.TimeoutExpired:error='child_exit_unobserved'
    except OSError as exc:error=exc.errno
    finally:
        selector.close()
        for pipe in [child.stdout,child.stderr]:
            if not pipe.closed:pipe.close()
    status=child.poll()
    if overflow is not None or error is not None or status!=0:
        raise CaptureRefused({'exit_code':status,'signal':-status if status is not None and status<0 else None,'overflow_stream':overflow,'error':error,'child_exit_observed':status is not None,'stdout_bytes':len(streams['stdout']),'stderr_bytes':len(streams['stderr']),'outer_cleanup_required':True,'started':True})
    return bytes(streams['stdout'])

# The total gzip expansion includes metadata, headers and padding. A complete
# bounded spool and header preflight exist BEFORE tarfile may process PAX/GNU
# headers. BoundedTarReader additionally refuses oversized parser read requests.
MAX_TAR_BYTES=512<<20
MAX_METADATA_BYTES=1<<20
MAX_METADATA_TOTAL=4<<20
MAX_METADATA_HEADERS=128
# Explicit accepted PAX contract. None of these fields changes body traversal.
# All size, GNU.sparse.*, SCHILY/vendor and charset-control forms refuse rather
# than silently omitting files or relying on parser-specific effective offsets.
PAX_NONOFFSET_KEYS=frozenset({'comment','path','linkpath','mtime','atime','ctime','uid','gid','uname','gname'})
def inspect_pax(body):
    offset=0
    while offset<len(body):
        # A record is "LEN KEY=VALUE\n", with LEN counting its own digits.
        # The entire declared body and padding are bounded before this function.
        space=body.find(b' ',offset,min(offset+9,len(body)))
        require(space>offset)
        digits=body[offset:space]
        require(digits.isdigit() and digits[:1]!=b'0')
        length=int(digits);end=offset+length
        require(end<=len(body) and end>space+2 and body[end-1:end]==b'\n')
        record=body[space+1:end-1];equals=record.find(b'=')
        require(equals>0)
        key=record[:equals].decode('ascii')
        require(key in PAX_NONOFFSET_KEYS)
        # Force a single supported decoding; no hdrcharset/BINARY fallback.
        record[equals+1:].decode('utf-8')
        offset=end
    require(offset==len(body))
class BoundedTarReader:
    def __init__(self,stream,size):self.stream,self.size=stream,size
    def tell(self):return self.stream.tell()
    def seek(self,offset,whence=0):
        require(whence in (0,1,2))
        target=offset if whence==0 else self.tell()+offset if whence==1 else self.size+offset
        require(0<=target<=self.size);return self.stream.seek(target)
    def read(self,size=-1):
        require(0<=size<=MAX_METADATA_BYTES and size<=self.size-self.tell())
        return self.stream.read(size)
def bounded_tar(raw,path,budget=None,traversal=None):
    if budget is None:budget=MAX_TAR_BYTES
    require(0<budget<=MAX_TAR_BYTES)
    require(len(raw)<=128<<20)
    with path.open('xb') as output,gzip.GzipFile(fileobj=io.BytesIO(raw),mode='rb') as source:
        total=0
        while True:
            block=source.read(min(65536,budget-total+1))
            if not block:break
            total+=len(block);require(total<=budget)
            require(output.write(block)==len(block))
    require(total>=1024 and total%512==0)
    metadata_count=0;metadata_total=0;header_count=0
    with path.open('rb') as source:
        while source.tell()<total:
            header=source.read(512);require(len(header)==512)
            if header==bytes(512):
                # Require the second end block and only bounded zero padding.
                require(source.read(512)==bytes(512))
                while True:
                    tail=source.read(65536)
                    if not tail:break
                    require(not any(tail))
                break
            # frombuf parses only this fixed512-byte header, never extended data.
            info=tarfile.TarInfo.frombuf(header,'utf-8','surrogateescape')
            header_count+=1;require(header_count<=30000 and 0<=info.size<=256<<20)
            data_offset=source.tell()
            padded=((info.size+511)//512)*512
            require(padded<=total-source.tell())
            metadata=info.type in (tarfile.XHDTYPE,tarfile.XGLTYPE,tarfile.GNUTYPE_LONGNAME,tarfile.GNUTYPE_LONGLINK)
            if metadata:
                metadata_count+=1;metadata_total+=info.size
                require(info.size<=MAX_METADATA_BYTES and metadata_count<=MAX_METADATA_HEADERS and metadata_total<=MAX_METADATA_TOTAL)
                data=source.read(padded);require(len(data)==padded)
                # tarfile reads whole padded metadata blocks. Hidden records in
                # padding must not bypass inspection of the logical PAX body.
                require(not any(data[info.size:]))
                if info.type in (tarfile.XHDTYPE,tarfile.XGLTYPE):inspect_pax(data[:info.size])
            elif info.type in (tarfile.REGTYPE,tarfile.AREGTYPE):
                if traversal is not None:traversal[data_offset]=(info.size,info.type)
                source.seek(padded,1)
            elif info.type in (tarfile.DIRTYPE,tarfile.SYMTYPE,tarfile.LNKTYPE):
                # The extraction parser does not skip bodies for these forms.
                # Requiring zero raw size prevents another raw/effective offset
                # mismatch. npm still rejects links during extraction.
                require(info.size==0)
                if traversal is not None:traversal[data_offset]=(0,info.type)
            else:
                # Explicitly includes GNU old sparse (S), contiguous/vendor and
                # unknown forms. Their offsets cannot silently enter this parser.
                raise Refused('unsupported_tar_traversal')
        else:raise Refused('missing_tar_end')
    return total
def read_member(source,member,limit):
    require(0<=member.size<=limit);stream=source.extractfile(member);require(stream is not None)
    parts=[];remaining=member.size
    while remaining:
        block=stream.read(min(65536,remaining));require(block and len(block)<=remaining)
        parts.append(block);remaining-=len(block)
    require(not stream.read(1));return b''.join(parts)
def verify_node_signature(entry,lock,work):
    sums=fetch(entry['shasums_url'],65536,sha256=entry['shasums_sha256'])
    signature=fetch(entry['signature_url'],65536,sha256=entry['signature_sha256'])
    keyring=fetch(lock['public_keyring']['url'],65536,sha256=lock['public_keyring']['sha256'])
    for name,raw in [('sums',sums),('sums.sig',signature),('public-release.kbx',keyring)]:write(work/name,raw)
    home=work/'gpg-home';home.mkdir(mode=0o700)
    status=command(['/usr/bin/gpgv','--homedir',str(home),'--keyring',str(work/'public-release.kbx'),'--status-fd','1',str(work/'sums.sig'),str(work/'sums')],work).decode('ascii')
    # gpgv trusts the supplied keyring. The reviewed official active-key snapshot
    # and exact fingerprint are trust inputs, not a claim of live revocation proof.
    valid=[line.split() for line in status.splitlines() if line.startswith('[GNUPG:] VALIDSIG ')]
    require(len(valid)==1 and lock['node_primary_fingerprint'] in (valid[0][2],valid[0][-1]))
    require(not any(flag in status for flag in ['REVKEYSIG','EXPKEYSIG','BADSIG','ERRSIG']))
    filename=entry['url'].rsplit('/',1)[1]
    require(sum(line==entry['sha256']+'  '+filename for line in sums.decode('ascii').splitlines())==1)
def verify_npm_signature(version,entry,lock,work):
    key=lock['npm_verification_key'];require(key['expires'] is None and key['scheme']=='ecdsa-sha2-nistp256' and key['keytype']=='ecdsa-sha2-nistp256')
    # Use OpenSSL's maintained verifier, not a custom implementation. Registry
    # keyid is the pinned registry identifier; DER bytes are independently pinned
    # in the reviewed release lock, not inferred from that label.
    pem=b'-----BEGIN PUBLIC KEY-----\n'+key['key'].encode()+b'\n-----END PUBLIC KEY-----\n'
    write(work/'npm-public.pem',pem)
    write(work/'npm-message',('npm@'+version+':'+entry['integrity']).encode())
    require(0<len(entry['signatures'])<=4)
    for i,signature in enumerate(entry['signatures']):
        require(signature['keyid']==key['keyid'])
        path=work/('npm-signature-'+str(i));write(path,base64.b64decode(signature['sig'],validate=True))
        command(['/usr/bin/openssl','dgst','-sha256','-verify',str(work/'npm-public.pem'),'-signature',str(path),str(work/'npm-message')],work)
def members(archive,traversal):
    count=0;total=0;seen=set();offsets=set()
    for member in archive:
        count+=1;total+=member.size;require(count<=20000 and total<=1<<30 and member.size<=256<<20)
        # The general parser may neither invent nor reinterpret a body span.
        # Accepted metadata is non-offset only; the raw preflight is authority.
        require(member.offset_data not in offsets and traversal.get(member.offset_data)==(member.size,member.type))
        offsets.add(member.offset_data)
        name=safe_name(member.name);require(name not in seen);seen.add(name)
        yield name,member
    require(offsets==set(traversal))
def extract_node(raw,member_name,dest,work):
    archive=work/'node.tar';traversal={};size=bounded_tar(raw,archive,traversal=traversal);found=False
    with archive.open('rb') as underlying,tarfile.open(fileobj=BoundedTarReader(underlying,size),mode='r:') as source:
        for name,member in members(source,traversal):
            if name!=member_name:continue
            require(not found and member.isfile() and not member.issym() and not member.islnk());found=True
            node=read_member(source,member,256<<20)
            # ELF64, little-endian, x86-64. Native runtime query follows after
            # authenticated archive verification; cross-arch/emulation refuses.
            require(len(node)>=64 and node[:5]==b'\x7fELF\x02' and node[5]==1 and struct.unpack('<H',node[18:20])[0]==62)
            write(dest/'bin/node',node,True)
    require(found)
def extract_npm(raw,entry,dest,work,expected_version):
    archive=work/'npm.tar';traversal={};size=bounded_tar(raw,archive,traversal=traversal);count=0;total=0
    target=dest/'lib/node_modules/npm';target.mkdir(parents=True,mode=0o700)
    with archive.open('rb') as underlying,tarfile.open(fileobj=BoundedTarReader(underlying,size),mode='r:') as source:
        for name,member in members(source,traversal):
            require(name=='package' or name.startswith('package/'))
            require(not member.issym() and not member.islnk() and (member.isfile() or member.isdir()))
            if name=='package':require(member.isdir());continue
            rel=safe_name(name[len('package/'):]);require('.git' not in PurePosixPath(rel).parts)
            if member.isdir():(target/rel).mkdir(parents=True,exist_ok=True,mode=0o700);continue
            content=read_member(source,member,64<<20)
            count+=1;total+=len(content);write(target/rel,content)
    require(count==entry['file_count'] and total==entry['unpacked_size'])
    package=json.loads((target/'package.json').read_bytes());require(package['name']=='npm' and package['version']==expected_version)
    require((target/'bin/npm-cli.js').is_file())
def run(mode,work,parent):
    require(mode in ('qualify','stage') and sys.platform=='linux' and platform.machine()=='x86_64')
    require(work.is_absolute() and parent.is_absolute() and work.is_dir() and work.parent==parent)
    require(not work.is_symlink() and work.stat().st_mode&0o077==0)
    lock_bytes=(HERE/'release-lock.json').read_bytes();lock=json.loads(lock_bytes)
    require(lock['schema_version']==1 and lock['platform']=='linux-x64' and set(lock['tools'])=={'root','runtime'})
    qualification=json.loads((HERE/'qualified.json').read_bytes())
    script_sha=digest(Path(__file__).read_bytes())
    if mode=='stage':
        require(qualification['approved'] is True and qualification['platform']=='linux-x64')
        require(qualification['release_lock_sha256']==digest(lock_bytes) and qualification['stage_script_sha256']==script_sha)
        receipt=qualification['provider_receipt'];require(isinstance(receipt,dict))
        require(receipt['repository']=='keeldock-platform/ci-kit' and receipt['workflow_path']=='.github/workflows/ci-policy.yml' and receipt['conclusion']=='success')
        require(all(isinstance(receipt[k],str) and re.fullmatch('[0-9]+',receipt[k]) for k in ['run_id','attempt','artifact_id']))
        require(re.fullmatch('[a-f0-9]{40}',receipt['kit_sha']))
    payload=work/'payload';payload.mkdir(mode=0o700);tools={}
    for role,versions in [('root',('24.22.1','11.19.1')),('runtime',('24.22.2','12.2.2'))]:
        entry=lock['tools'][role];require((entry['node_version'],entry['npm_version'])==versions)
        temp=work/(role+'-verification');temp.mkdir(mode=0o700)
        verify_node_signature(entry['node'],lock,temp)
        verify_npm_signature(entry['npm_version'],entry['npm'],lock,temp)
        node=fetch(entry['node']['url'],128<<20,sha256=entry['node']['sha256'])
        npm=fetch(entry['npm']['url'],32<<20,integrity=entry['npm']['integrity'])
        dest=payload/role;dest.mkdir(mode=0o700)
        extract_node(node,entry['node']['archive_member'],dest,temp)
        extract_npm(npm,entry['npm'],dest,temp,entry['npm_version'])
        npm_root=dest/'lib/node_modules/npm';before=tree(npm_root);node_before=digest((dest/'bin/node').read_bytes())
        require(command([str(dest/'bin/node'),'--version'],temp).strip()==('v'+entry['node_version']).encode())
        probe=json.loads(command([str(dest/'bin/node'),'-p','JSON.stringify({platform:process.platform,arch:process.arch})'],temp))
        require(probe=={'platform':'linux','arch':'x64'})
        require(command([str(dest/'bin/node'),str(npm_root/'bin/npm-cli.js'),'--version','--ignore-scripts','--userconfig=/dev/null','--globalconfig=/dev/null'],temp).strip()==entry['npm_version'].encode())
        require(tree(npm_root)==before and digest((dest/'bin/node').read_bytes())==node_before)
        tools[role]={'node':role+'/bin/node','npm_root':role+'/lib/node_modules/npm','node_sha256':node_before,'npm_tree_sha256':digest(canonical(before)),'node_version':entry['node_version'],'npm_version':entry['npm_version']}
    if mode=='stage':
        require(tools==qualification['tools'])
        target=parent/'ci-lock-tools';require(not target.exists() and not target.is_symlink())
        os.rename(payload,target)
    result={'schema_version':1,'platform':'linux-x64','release_lock_sha256':digest(lock_bytes),'stage_script_sha256':script_sha,'tools':tools,'verification':'Official archive SHA256 and Node detached signature plus npm SHA512 and all pinned ECDSA signatures; native versions and unchanged complete npm trees'}
    write(work/'qualification.json',json.dumps(result,sort_keys=True,indent=2).encode()+b'\n')
if __name__=='__main__':
    try:
        require(len(sys.argv)==4);run(sys.argv[1],Path(sys.argv[2]),Path(sys.argv[3]))
    except CaptureRefused as error:
        record=(json.dumps({'tool_child_outcome':error.record},sort_keys=True,separators=(',',':'))+'\n').encode()
        # Fixed bounded structured private record, no environment or raw streams.
        try:
            require(len(record)<=4096)
            while record:
                count=os.write(2,record);require(count>0);record=record[count:]
        except Exception:pass
        sys.exit(6)
    except Exception:sys.exit(6)
