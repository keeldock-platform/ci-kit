// Package sourceoutput is a PROPOSED explicit private SOURCE return capability.
// It is deliberately separate from failure diagnostics. Not wired or qualified.
package sourceoutput

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "reflect"
 "sort"
 "strings"
 "time"

 "filippo.io/age"
 "github.com/keeldock-platform/ci-kit/internal/process"
)

const MaxLock = 16 << 20
const MaxEnvelope = 48 << 20
var denied = errors.New("source_output_refused") // no private detail in public errors
var paths = []string{"package-lock.json", "runtime/package-lock.json"}
// No caller-selected commands, output paths, registry, refs, toolchain or recipient.
// Bind is constructed ONLY from the validated immutable request/acquisition and
// verified provider receipt, never decoded directly from untrusted source files.
type Bind struct {
 SourceSHA string `json:"source_sha"`
 BaseSHA string `json:"base_sha"`
 KitSHA string `json:"kit_sha"`
 ControlSHA string `json:"control_sha"`
 RequestID string `json:"request_id"`
 RequestDigest string `json:"request_digest"`
 RunID string `json:"run_id"`
 Attempt int `json:"attempt"`
}
type Tool struct {
 Node string `json:"node"` // staged absolute executable, outside acquisition
 NodeSHA256 string `json:"node_sha256"`
 NpmRoot string `json:"npm_root"` // staged complete npm tree, outside acquisition
 NpmTreeSHA256 string `json:"npm_tree_sha256"`
 NodeVersion string `json:"node_version"`
 NpmVersion string `json:"npm_version"`
}
type Policy struct {
 Enabled bool `json:"enabled"`
 Purpose string `json:"purpose"`
 Bind Bind `json:"bind"`
 ManifestSHA256 string `json:"manifest_sha256"` // canonical all-package.json map
 Recipient string `json:"recipient"` // explicit source-purpose approval required
 Root Tool `json:"root"`
 Runtime Tool `json:"runtime"`
}
type Bundle struct {
 AuthorizedRecipient string `json:"authorized_recipient"`
 Schema int `json:"schema_version"`
 Purpose string `json:"purpose"`
 Bind Bind `json:"bind"`
 Manifests map[string]string `json:"manifests"`
 Root Tool `json:"root_toolchain"`
 Runtime Tool `json:"runtime_toolchain"`
 Files map[string][]byte `json:"source_files"`
 SHA256 map[string]string `json:"source_sha256"`
}
func digest(b []byte) string { s:=sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func fullHex(s string,n int) bool { if len(s)!=n{return false}; b,e:=hex.DecodeString(s); return e==nil && hex.EncodeToString(b)==s }
func validBind(b Bind) bool {
 for _,s:=range []string{b.SourceSHA,b.BaseSHA,b.KitSHA,b.ControlSHA}{if !fullHex(s,40){return false}}
 if !fullHex(b.RequestDigest,64)|| b.Attempt<1 || len(b.RunID)<1 || len(b.RunID)>24{return false}
 for _,r:=range b.RunID {if r<'0'||r>'9'{return false}}
 // Dispatch.Validate must already have validated the exact UUID and request digest.
 return len(b.RequestID)==36
}
// snapshot includes ignored files and rejects links/special objects. .git is
// acquired control metadata, never a source result. Hard-linked files are not
// accepted by integration: acquisition must be a fresh private ordinary tree.
func snapshot(root string) (map[string]string,error) {
 out:=map[string]string{}; var total int64
 err:=filepath.WalkDir(root,func(path string,d os.DirEntry,e error) error {
  if e!=nil{return denied}; rel,e:=filepath.Rel(root,path);if e!=nil{return denied}
  if rel=="."{return nil}; if rel==".git"{if !d.IsDir()||d.Type()&os.ModeSymlink!=0{return denied};return filepath.SkipDir}
  if d.Type()&os.ModeSymlink!=0{return denied};if d.IsDir(){return nil}
  info,e:=d.Info();if e!=nil||!info.Mode().IsRegular()||info.Size()>64<<20{return denied}
  total+=info.Size();if total>1<<30||len(out)>=100000{return denied}
  f,e:=os.Open(path);if e!=nil{return denied};h:=sha256.New();_,e=io.Copy(h,io.LimitReader(f,64<<20+1));ce:=f.Close();if e!=nil||ce!=nil{return denied}
  out[filepath.ToSlash(rel)]=hex.EncodeToString(h.Sum(nil));return nil
 });return out,err
}
func mapDigest(m map[string]string) string {b,_:=json.Marshal(m);return digest(b)}
func manifestMap(m map[string]string) map[string]string {
 out:=map[string]string{};for p,h:=range m{if p=="package.json"||strings.HasSuffix(p,"/package.json"){out[p]=h}};return out
}
func inside(root,path string) bool {r,e:=filepath.Rel(root,path);return e==nil && r!=".."&&!strings.HasPrefix(r,".."+string(os.PathSeparator))}
func tool(t Tool,root string) error {
 if !filepath.IsAbs(t.Node)||!filepath.IsAbs(t.NpmRoot)||inside(root,t.Node)||inside(root,t.NpmRoot){return denied}
 if _,e:=process.SafePath(filepath.Dir(t.Node),".");e!=nil{return denied};if _,e:=process.SafePath(t.NpmRoot,".");e!=nil{return denied}
 st,e:=os.Lstat(t.Node);if e!=nil||!st.Mode().IsRegular()||st.Size()>256<<20{return denied}
 raw,e:=os.ReadFile(t.Node);if e!=nil||digest(raw)!=t.NodeSHA256{return denied}
 tree,e:=snapshot(t.NpmRoot);if e!=nil||mapDigest(tree)!=t.NpmTreeSHA256{return denied}
 if _,ok:=tree["bin/npm-cli.js"];!ok{return denied};return nil
}
func onlyLocks(before,after map[string]string) bool {
 a:=map[string]string{};b:=map[string]string{};for p,h:=range before{a[p]=h};for p,h:=range after{b[p]=h}
 for _,p:=range paths{delete(a,p);delete(b,p)};return reflect.DeepEqual(a,b)
}
func copyTree(root,dest string,expected map[string]string) error {
 if e:=os.Mkdir(dest,0700);e!=nil{return denied}
 keys:=make([]string,0,len(expected));for p:=range expected{keys=append(keys,p)};sort.Strings(keys)
 for _,p:=range keys{
  raw,e:=os.ReadFile(filepath.Join(root,filepath.FromSlash(p)));if e!=nil||digest(raw)!=expected[p]{return denied}
  target:=filepath.Join(dest,filepath.FromSlash(p));if os.MkdirAll(filepath.Dir(target),0700)!=nil{return denied}
  f,e:=os.OpenFile(target,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if e!=nil{return denied};n,e:=f.Write(raw);ce:=f.Close();if e!=nil||ce!=nil||n!=len(raw){return denied}
 };return nil
}
func lock(root,p string) ([]byte,error) {
 st,e:=os.Lstat(filepath.Join(root,p));if e!=nil||!st.Mode().IsRegular()||st.Size()<2||st.Size()>MaxLock{return nil,denied}
 b,e:=os.ReadFile(filepath.Join(root,p));if e!=nil{return nil,denied}
 var v struct{LockfileVersion int `json:"lockfileVersion"`;Packages map[string]json.RawMessage `json:"packages"`}
 if json.Unmarshal(b,&v)!=nil||v.LockfileVersion!=3||len(v.Packages)==0{return nil,denied};if _,ok:=v.Packages[""];!ok{return nil,denied};return b,nil
}
func npm(ctx context.Context,root,capture,cwd string,t Tool,install bool) error {
 // Network is explicit fixed argv for the registry read, never a secret-bearing
 // injected environment. No lifecycle scripts, audit/fund, builds or arbitrary args.
 home:=capture+"-home";if process.MakePrivateDir(home)!=nil{return denied}
 args:=[]string{t.Node,filepath.Join(t.NpmRoot,"bin/npm-cli.js"),"install","--package-lock-only","--ignore-scripts","--offline=false","--registry=https://registry.npmjs.org","--no-audit","--no-fund"}
 if install{args=[]string{t.Node,filepath.Join(t.NpmRoot,"bin/npm-cli.js"),"ci","--ignore-scripts","--offline=false","--registry=https://registry.npmjs.org","--no-audit","--no-fund"}}
 args=append(args,"--userconfig="+os.DevNull,"--globalconfig="+os.DevNull,"--cache="+filepath.Join(home,"cache"))
 result,e:=process.Run(ctx,process.Spec{Argv:args,Root:root,Cwd:cwd,ResultDir:capture,Timeout:15*time.Minute,Env:map[string]string{"HOME":home,"USERPROFILE":home,"TMPDIR":home,"PATH":filepath.Dir(t.Node)}})
 if e!=nil||result.ExitCode!=0||!result.CleanupComplete||result.TimedOut||result.Overflow{return denied};return nil
}
// Generate returns source bytes only in memory to Seal, never stdout/JUnit.
// All setup/tool staging occurs through separately reviewed existing pack policy.
func Generate(ctx context.Context,root,private string,p Policy,actual Bind) (Bundle,error) {
 var empty Bundle
 if !p.Enabled||p.Purpose!="dependency-lock-source-v1"||!validBind(actual)||p.Bind!=actual||!fullHex(p.ManifestSHA256,64){return empty,denied}
 if p.Root.NodeVersion!="24.22.1"||p.Root.NpmVersion!="11.19.1"||p.Runtime.NodeVersion!="24.22.2"||p.Runtime.NpmVersion!="12.2.2"{return empty,denied}
 if _,e:=canonicalRecipient(p.Recipient);e!=nil{return empty,denied}
 if !filepath.IsAbs(root)||!filepath.IsAbs(private)||inside(root,private)||!process.UserOnly(private,true){return empty,denied}
 if tool(p.Root,root)!=nil||tool(p.Runtime,root)!=nil{return empty,denied}
 before,e:=snapshot(root);if e!=nil{return empty,denied}; manifests:=manifestMap(before)
 if mapDigest(manifests)!=p.ManifestSHA256{return empty,denied}
 for i,cwd:=range []string{".","runtime"}{t:=p.Root;if i==1{t=p.Runtime};if npm(ctx,root,filepath.Join(private,"lock-generate-"+cwd),cwd,t,false)!=nil{return empty,denied}}
 after,e:=snapshot(root);if e!=nil||!onlyLocks(before,after){return empty,denied}
 b:=Bundle{AuthorizedRecipient:p.Recipient,Schema:1,Purpose:p.Purpose,Bind:actual,Manifests:manifests,Root:toolIdentity(p.Root),Runtime:toolIdentity(p.Runtime),Files:map[string][]byte{},SHA256:map[string]string{}}
 for _,path:=range paths{raw,e:=lock(root,path);if e!=nil{return empty,denied};b.Files[path]=raw;b.SHA256[path]=digest(raw)}
 // A second generation pass must be byte-stable, then npm ci validates each
 // generated graph in a separate private source copy. The acquired source remains
 // limited to the two lock deltas; node_modules never becomes a source result.
 for i,cwd:=range []string{".","runtime"}{t:=p.Root;if i==1{t=p.Runtime};if npm(ctx,root,filepath.Join(private,"lock-replay-"+cwd),cwd,t,false)!=nil{return empty,denied}}
 replay,e:=snapshot(root);if e!=nil||!reflect.DeepEqual(after,replay){return empty,denied}
 validation:=filepath.Join(private,"lock-validation");if copyTree(root,validation,after)!=nil{return empty,denied}
 defer os.RemoveAll(validation)
 for i,cwd:=range []string{".","runtime"}{t:=p.Root;if i==1{t=p.Runtime};if npm(ctx,validation,filepath.Join(private,"lock-ci-"+cwd),cwd,t,true)!=nil{return empty,denied}}
 // Verify every original source file after validation. Install-created files are
 // isolated in validation and are never collected/returned, even on failure.
 for path,h:=range after{raw,e:=os.ReadFile(filepath.Join(validation,path));if e!=nil||digest(raw)!=h{return empty,denied}}
 end,e:=snapshot(root);if e!=nil||!reflect.DeepEqual(after,end)||tool(p.Root,root)!=nil||tool(p.Runtime,root)!=nil{return empty,denied}
 return b,nil
}
// Seal writes one explicit source-output envelope into a private fresh directory.
// Caller publishes readiness ONLY after acquisition and private cleanup succeed.
func Seal(dir string,b Bundle,recipient string) (string,error) {
 if !validBundle(b)||!process.UserOnly(dir,true){return "",denied}
 r,e:=canonicalRecipient(recipient);if e!=nil||b.AuthorizedRecipient!=recipient{return "",denied}
 f,e:=os.OpenFile(filepath.Join(dir,"locks.age.partial"),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if e!=nil{return "",denied}
 partial:=f.Name();defer os.Remove(partial)
 w,e:=age.Encrypt(f,r);if e!=nil{f.Close();return "",denied}
 e=json.NewEncoder(&boundedWriter{w:w,left:MaxEnvelope}).Encode(b);closeErr:=w.Close();syncErr:=f.Sync();fileErr:=f.Close()
 if e!=nil||closeErr!=nil||syncErr!=nil||fileErr!=nil{return "",denied}
 target:=filepath.Join(dir,"locks.age");if os.Link(partial,target)!=nil{return "",denied};return target,nil
}
func validBundle(b Bundle) bool {
 if _,e:=canonicalRecipient(b.AuthorizedRecipient);e!=nil{return false}
 if b.Schema!=1||b.Purpose!="dependency-lock-source-v1"||!validBind(b.Bind)||len(b.Files)!=2||len(b.SHA256)!=2||len(b.Manifests)<2{return false}
 if _,ok:=b.Manifests["package.json"];!ok{return false};if _,ok:=b.Manifests["runtime/package.json"];!ok{return false}
 for _,p:=range paths{v,ok:=b.Files[p];if !ok||len(v)<2||len(v)>MaxLock||!validLockBytes(v)||digest(v)!=b.SHA256[p]{return false}};return true
}
// Open is an owner-local primitive, NOT provider receipt verification. The
// retrieval command must FIRST bind the exact successful provider run/attempt,
// current trusted registration/kit/control/source/base/request and artifact ID.
// It must then compare the envelope bind and manifest/tool tuple to expected.
// No source output is printed or written by this primitive.
func Open(cipher io.Reader,identity age.Identity,expected Policy) (Bundle,error) {
 var b Bundle
 if !expected.Enabled||expected.Purpose!="dependency-lock-source-v1"||ValidateRecipientIdentity(expected.Recipient,identity)!=nil{return b,denied}
 plain,e:=age.Decrypt(io.LimitReader(cipher,MaxEnvelope+1),identity);if e!=nil{return b,denied}
 raw,e:=io.ReadAll(io.LimitReader(plain,MaxEnvelope+1));if e!=nil||len(raw)>MaxEnvelope{return b,denied}
 d:=json.NewDecoder(bytes.NewReader(raw));d.DisallowUnknownFields();if d.Decode(&b)!=nil||!validBundle(b)||b.AuthorizedRecipient!=expected.Recipient||b.Bind!=expected.Bind||mapDigest(b.Manifests)!=expected.ManifestSHA256||b.Root!=toolIdentity(expected.Root)||b.Runtime!=toolIdentity(expected.Runtime){return Bundle{},denied}
 var extra any;if d.Decode(&extra)!=io.EOF{return Bundle{},denied};return b,nil
}

func toolIdentity(t Tool) Tool { t.Node="";t.NpmRoot="";return t }
func ValidateSource(root string,p Policy,actual Bind) error {
 if !p.Enabled||p.Purpose!="dependency-lock-source-v1"||!validBind(actual)||p.Bind!=actual{return denied}
 if _,e:=canonicalRecipient(p.Recipient);e!=nil{return denied}
 m,e:=snapshot(root);if e!=nil||mapDigest(manifestMap(m))!=p.ManifestSHA256{return denied};return nil
}

func validLockBytes(v []byte) bool { var x struct { LockfileVersion int `json:"lockfileVersion"`;Packages map[string]json.RawMessage `json:"packages"` };if json.Unmarshal(v,&x)!=nil||x.LockfileVersion!=3||len(x.Packages)==0{return false};_,ok:=x.Packages[""];return ok }

type boundedWriter struct { w io.Writer; left int }
func (w *boundedWriter) Write(p []byte) (int,error) { if len(p)>w.left{return 0,denied};n,e:=w.w.Write(p);w.left-=n;if e==nil&&n!=len(p){e=io.ErrShortWrite};return n,e }

// Only the existing native X25519 recipient contract is admitted. Parse and
// reserialize through age itself; alternate spelling/whitespace is refused.
func canonicalRecipient(s string) (*age.X25519Recipient,error) {
 r,e:=age.ParseX25519Recipient(s);if e!=nil||r.String()!=s{return nil,denied};return r,nil
}
// The owner-local expectation is authority; the envelope never selects the key.
// This capability admits one native X25519 identity, not opaque/plugin identities.
func ValidateRecipientIdentity(recipient string,identity age.Identity) error {
 r,e:=canonicalRecipient(recipient);if e!=nil{return denied}
 id,ok:=identity.(*age.X25519Identity);if !ok||id==nil||id.Recipient().String()!=r.String(){return denied};return nil
}
