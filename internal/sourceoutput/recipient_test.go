package sourceoutput

// Source-free approved-venue fixtures. These functions are AUTHORED, NOT RUN.
// Identities are ephemeral synthetic fixtures generated only when tests run;
// no real identity, recipient authority, source tree, npm or provider is used.
import (
 "bytes"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "reflect"
 "strings"
 "testing"

 "filippo.io/age"
 "github.com/keeldock-platform/ci-kit/internal/process"
)

func cryptoFixture(t *testing.T) (Bundle,Policy,*age.X25519Identity) {
 t.Helper()
 id,e:=age.GenerateX25519Identity();if e!=nil{t.Fatal(e)}
 b:=fixture();b.AuthorizedRecipient=id.Recipient().String()
 b.Root=Tool{NodeSHA256:strings.Repeat("a",64),NpmTreeSHA256:strings.Repeat("b",64),NodeVersion:"24.22.1",NpmVersion:"11.19.1"}
 b.Runtime=Tool{NodeSHA256:strings.Repeat("c",64),NpmTreeSHA256:strings.Repeat("d",64),NodeVersion:"24.22.2",NpmVersion:"12.2.2"}
 for i:=0;i<15;i++{b.Manifests[fmt.Sprintf("packages/synthetic-%02d/package.json",i)]=strings.Repeat("e",64)}
 p:=Policy{Enabled:true,Purpose:b.Purpose,Bind:b.Bind,ManifestSHA256:mapDigest(b.Manifests),Recipient:b.AuthorizedRecipient,Root:b.Root,Runtime:b.Runtime}
 return b,p,id
}
func sealed(t *testing.T,b Bundle) []byte {
 t.Helper()
 root,e:=filepath.EvalSymlinks(t.TempDir());if e!=nil{t.Fatal(e)}
 dir:=filepath.Join(root,"cipher-only");if process.MakePrivateDir(dir)!=nil{t.Fatal("private fixture directory")}
 path,e:=Seal(dir,b,b.AuthorizedRecipient);if e!=nil{t.Fatal(e)}
 raw,e:=os.ReadFile(path);if e!=nil{t.Fatal(e)};return raw
}
func assertClosed(t *testing.T,raw []byte,id age.Identity,p Policy) {
 t.Helper();got,e:=Open(bytes.NewReader(raw),id,p)
 if e==nil||!reflect.DeepEqual(got,Bundle{}){t.Fatal("refusal returned source or success")}
}
func TestSealOpenMatchingAuthorizedRecipient(t *testing.T) {
 b,p,id:=cryptoFixture(t)
 got,e:=Open(bytes.NewReader(sealed(t,b)),id,p)
 if e!=nil||!reflect.DeepEqual(got,b)||len(got.Manifests)!=17{t.Fatal("matching native recipient round trip failed")}
}
func TestOpenSeparatelyApprovedRecipient(t *testing.T) {
 b,p,id:=cryptoFixture(t);raw:=sealed(t,b)
 other,e:=age.GenerateX25519Identity();if e!=nil{t.Fatal(e)}
 for _,tc:=range []struct{name,recipient string}{
  {"different-valid",other.Recipient().String()},
  {"empty",""},{"malformed","not-an-age-recipient"},
  {"noncanonical-space",p.Recipient+" "},{"noncanonical-case",strings.ToUpper(p.Recipient)},
 }{t.Run(tc.name,func(t *testing.T){expected:=p;expected.Recipient=tc.recipient;assertClosed(t,raw,id,expected)})}
 t.Run("wrong-selected-identity",func(t *testing.T){assertClosed(t,raw,other,p)})
 t.Run("nil-selected-identity",func(t *testing.T){assertClosed(t,raw,nil,p)})
 var typedNil *age.X25519Identity
 t.Run("typed-nil-selected-identity",func(t *testing.T){assertClosed(t,raw,typedNil,p)})
}
func TestSealRejectsConflictingRecipientWithoutPublication(t *testing.T) {
 b,_,_:=cryptoFixture(t);other,e:=age.GenerateX25519Identity();if e!=nil{t.Fatal(e)}
 for _,tc:=range []struct{name,argument,envelope string}{
  {"argument-B-envelope-A",other.Recipient().String(),b.AuthorizedRecipient},
  {"argument-A-envelope-B",b.AuthorizedRecipient,other.Recipient().String()},
  {"empty-argument","",b.AuthorizedRecipient},
  {"malformed-argument","bad",b.AuthorizedRecipient},
  {"empty-envelope",b.AuthorizedRecipient,""},
 }{t.Run(tc.name,func(t *testing.T){
  root,e:=filepath.EvalSymlinks(t.TempDir());if e!=nil{t.Fatal(e)}
  dir:=filepath.Join(root,"private");if process.MakePrivateDir(dir)!=nil{t.Fatal("dir")}
  candidate:=b;candidate.AuthorizedRecipient=tc.envelope
  path,e:=Seal(dir,candidate,tc.argument);if e==nil||path!=""{t.Fatal("conflicting recipient sealed")}
  entries,e:=os.ReadDir(dir);if e!=nil||len(entries)!=0{t.Fatal("refused seal published or retained a partial")}
 })}
}
func TestOpenAgeTamperTruncationAndExactScopeReplay(t *testing.T) {
 b,p,id:=cryptoFixture(t);raw:=sealed(t,b)
 tampered:=append([]byte(nil),raw...);tampered[len(tampered)-1]^=1
 t.Run("age-authentication-tamper",func(t *testing.T){assertClosed(t,tampered,id,p)})
 t.Run("age-truncation",func(t *testing.T){assertClosed(t,raw[:len(raw)/2],id,p)})
 changes:=[]struct{name string;change func(*Policy)}{
  {"purpose",func(p *Policy){p.Purpose="failure-diagnostics"}},
  {"source",func(p *Policy){p.Bind.SourceSHA=strings.Repeat("9",40)}},
  {"base",func(p *Policy){p.Bind.BaseSHA=strings.Repeat("9",40)}},
  {"kit",func(p *Policy){p.Bind.KitSHA=strings.Repeat("9",40)}},
  {"control",func(p *Policy){p.Bind.ControlSHA=strings.Repeat("9",40)}},
  {"run",func(p *Policy){p.Bind.RunID="124"}},
  {"attempt",func(p *Policy){p.Bind.Attempt++}},
  {"request",func(p *Policy){p.Bind.RequestID="11111111-1111-4111-8111-111111111111"}},
  {"request-digest",func(p *Policy){p.Bind.RequestDigest=strings.Repeat("9",64)}},
  {"manifest-map",func(p *Policy){p.ManifestSHA256=strings.Repeat("9",64)}},
  {"root-tool",func(p *Policy){p.Root.NodeSHA256=strings.Repeat("9",64)}},
  {"runtime-tool",func(p *Policy){p.Runtime.NpmTreeSHA256=strings.Repeat("9",64)}},
 }
 for _,tc:=range changes{t.Run(tc.name,func(t *testing.T){expected:=p;tc.change(&expected);assertClosed(t,raw,id,expected)})}
}
// Anyone can encrypt to a public recipient. This intentionally bypasses Seal to
// prove Open refuses inconsistent authenticated envelope scope; provider receipt
// validation remains mandatory to establish the actual producer's origin.
func TestOpenRefusesForgedEnvelopeRecipientAndPurpose(t *testing.T) {
 b,p,id:=cryptoFixture(t);other,e:=age.GenerateX25519Identity();if e!=nil{t.Fatal(e)}
 for _,name:=range []string{"different-envelope-recipient","missing-envelope-recipient","wrong-envelope-purpose"}{
  t.Run(name,func(t *testing.T){candidate:=b
   switch name{case "different-envelope-recipient":candidate.AuthorizedRecipient=other.Recipient().String();case "missing-envelope-recipient":candidate.AuthorizedRecipient="";case "wrong-envelope-purpose":candidate.Purpose="failure-diagnostics"}
   var raw bytes.Buffer;w,e:=age.Encrypt(&raw,id.Recipient());if e!=nil{t.Fatal(e)}
   if json.NewEncoder(w).Encode(candidate)!=nil||w.Close()!=nil{t.Fatal("synthetic age fixture")}
   assertClosed(t,raw.Bytes(),id,p)
  })
 }
}
