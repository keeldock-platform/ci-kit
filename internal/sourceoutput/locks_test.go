package sourceoutput

// Proposed approved-CI fixtures only. NOT RUN on the owner Mac.
import (
 "context"
 "filippo.io/age"
 "strings"
 "testing"
)

func fixture() Bundle {
 id,e:=age.GenerateX25519Identity();if e!=nil{panic(e)}
 b:=Bundle{AuthorizedRecipient:id.Recipient().String(),Schema:1,Purpose:"dependency-lock-source-v1",Bind:Bind{SourceSHA:strings.Repeat("1",40),BaseSHA:strings.Repeat("2",40),KitSHA:strings.Repeat("3",40),ControlSHA:strings.Repeat("4",40),RequestID:"00000000-0000-4000-8000-000000000000",RequestDigest:strings.Repeat("5",64),RunID:"123",Attempt:1},Manifests:map[string]string{"package.json":strings.Repeat("6",64),"runtime/package.json":strings.Repeat("7",64)},Files:map[string][]byte{},SHA256:map[string]string{}}
 for _,p:=range paths {b.Files[p]=[]byte(`{"lockfileVersion":3,"packages":{"":{}}}`);b.SHA256[p]=digest(b.Files[p])}
 return b
}
func TestTwoSourceFilesOnly(t *testing.T) {
 cases:=[]struct{name string; change func(*Bundle); want bool}{
  {"positive",func(b *Bundle){},true},
  {"missing-runtime",func(b *Bundle){delete(b.Files,"runtime/package-lock.json")},false},
  {"additional-source",func(b *Bundle){b.Files["package.json"]=[]byte("{}");b.SHA256["package.json"]=digest([]byte("{}"))},false},
  {"path-traversal",func(b *Bundle){delete(b.Files,"package-lock.json");b.Files["../package-lock.json"]=[]byte("{}")},false},
  {"wrong-purpose",func(b *Bundle){b.Purpose="failure-diagnostics"},false},
  {"digest-drift",func(b *Bundle){b.Files["package-lock.json"]=[]byte("{}")},false},
  {"invalid-json",func(b *Bundle){b.Files["package-lock.json"]=[]byte("{");b.SHA256["package-lock.json"]=digest([]byte("{"))},false},
  {"lock-v2",func(b *Bundle){v:=[]byte(`{"lockfileVersion":2,"packages":{"":{}}}`);b.Files["package-lock.json"]=v;b.SHA256["package-lock.json"]=digest(v)},false},
  {"missing-manifest",func(b *Bundle){delete(b.Manifests,"runtime/package.json")},false},
  {"zero-attempt",func(b *Bundle){b.Bind.Attempt=0},false},
 }
 for _,c:=range cases{t.Run(c.name,func(t *testing.T){b:=fixture();c.change(&b);if validBundle(b)!=c.want{t.Fatal("source output refusal mismatch")}})}
}
func TestOnlyLockDrift(t *testing.T) {
 before:=map[string]string{"package.json":"a","runtime/package.json":"b","package-lock.json":"c","runtime/package-lock.json":"d"}
 after:=map[string]string{"package.json":"a","runtime/package.json":"b","package-lock.json":"e","runtime/package-lock.json":"f"}
 if !onlyLocks(before,after){t.Fatal("two-lock update rejected")}
 after["package.json"]="changed";if onlyLocks(before,after){t.Fatal("manifest drift accepted")}
 after["package.json"]="a";after[".npmrc"]="new";if onlyLocks(before,after){t.Fatal("extra output accepted")}
 delete(after,".npmrc");delete(after,"runtime/package.json");if onlyLocks(before,after){t.Fatal("deleted source accepted")}
}
func TestDisabledBeforeExecution(t *testing.T) {
 if _,e:=Generate(context.Background(),"/not-used","/not-used",Policy{},Bind{});e==nil{t.Fatal("disabled policy accepted")}
}
