package cli

// AUTHORED / NOT RUN. Exercises the actual retrieval method, real age Seal/Open,
// real go-gh request adapter and bounded in-memory provider/storage transports.
// These synthetic identities/tokens never select or authorize a real recipient.
import (
 "archive/zip"
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "filippo.io/age"
 gh "github.com/keeldock-platform/ci-kit/internal/github"
 "github.com/keeldock-platform/ci-kit/internal/process"
 "github.com/keeldock-platform/ci-kit/internal/protocol"
 "github.com/keeldock-platform/ci-kit/internal/sourceoutput"
)

type sourceRoundTrip func(*http.Request)(*http.Response,error)
func(f sourceRoundTrip)RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
func sourceReply(r *http.Request,value any)*http.Response{
 raw,e:=json.Marshal(value);if e!=nil{panic(e)}
 return &http.Response{Request:r,StatusCode:200,Header:http.Header{"Content-Type":[]string{"application/json"}},Body:io.NopCloser(bytes.NewReader(raw))}
}
func sourceHash(b []byte)string{h:=sha256.Sum256(b);return hex.EncodeToString(h[:])}
func sourceWrite(t *testing.T,path string,raw []byte){
 t.Helper();f,e:=process.CreatePrivateFile(path);if e!=nil{t.Fatal(e)}
 n,e:=f.Write(raw);ce:=f.Close();if e!=nil||ce!=nil||n!=len(raw){t.Fatal("private synthetic fixture write")}
}
func TestRetrieveSourceOutputRecipientProviderAndNoOutputOnRefusal(t *testing.T){
 for _,k:=range []string{"CI","GITHUB_ACTIONS","GITHUB_RUN_ID","GITHUB_OUTPUT","GITHUB_STEP_SUMMARY"}{t.Setenv(k,"")}
 cases:=[]string{"positive","recipient-B-identity-A","recipient-and-identity-B","empty-recipient","malformed-recipient","noncanonical-recipient","wrong-selected-identity","forged-envelope-recipient","tampered-ciphertext","replayed-attempt","wrong-provider-actor","wrong-provider-control","duplicate-artifact","expired-artifact","hostile-redirect","member-traversal","existing-output","parent-is-file","public-environment"}
 for _,name:=range cases{t.Run(name,func(t *testing.T){
  root,e:=filepath.EvalSymlinks(t.TempDir());if e!=nil{t.Fatal(e)}
  private:=filepath.Join(root,"private");if process.MakePrivateDir(private)!=nil{t.Fatal("private fixture directory")}
  id,e:=age.GenerateX25519Identity();if e!=nil{t.Fatal(e)}
  other,e:=age.GenerateX25519Identity();if e!=nil{t.Fatal(e)}
  d,e:=protocol.NewDispatch("core",strings.Repeat("a",40),strings.Repeat("b",40),"unit","source-output","linux-debian","all","11111111-1111-4111-8111-111111111111");if e!=nil{t.Fatal(e)}
  x:=gh.Expectation{SchemaVersion:1,CompositionSHA256:strings.Repeat("e",64),Registration:gh.Registration{SchemaVersion:1,Concern:"core",Source:gh.RegisteredRepository{Repository:"example/source",ID:1,TargetBranch:"main"},Caller:gh.RegisteredCaller{Repository:"example/checks",ID:2,WorkflowID:"3",WorkflowPath:".github/workflows/dispatch.yml",Actor:"example-admin"},ControlSHA:strings.Repeat("c",40),KitSHA:strings.Repeat("d",40),KitAPI:1,CapacityAvailable:true,Platforms:[]string{"linux-debian"},CompositionSHA256:strings.Repeat("e",64),SourceMapSHA256:strings.Repeat("f",64),SigningPolicy:"ssh-v1",Slots:[]string{"checks"}},Dispatch:d,RunID:"4",Attempt:1,CreatedAt:time.Now()}
  bind:=sourceoutput.Bind{SourceSHA:d.Inputs["source_sha"],BaseSHA:d.Inputs["base_sha"],KitSHA:x.Registration.KitSHA,ControlSHA:x.Registration.ControlSHA,RequestID:d.Inputs["request_id"],RequestDigest:d.Inputs["request_digest"],RunID:"4",Attempt:1}
  manifests:=map[string]string{"package.json":strings.Repeat("1",64),"runtime/package.json":strings.Repeat("2",64)}
  for i:=0;i<15;i++{manifests[fmt.Sprintf("packages/synthetic-%02d/package.json",i)]=strings.Repeat("3",64)}
  rootTool:=sourceoutput.Tool{NodeSHA256:strings.Repeat("4",64),NpmTreeSHA256:strings.Repeat("5",64),NodeVersion:"24.22.1",NpmVersion:"11.19.1"}
  runtimeTool:=sourceoutput.Tool{NodeSHA256:strings.Repeat("6",64),NpmTreeSHA256:strings.Repeat("7",64),NodeVersion:"24.22.2",NpmVersion:"12.2.2"}
  lock:=[]byte(`{"lockfileVersion":3,"packages":{"":{"name":"private-source-lock-sentinel"}}}`)
  bundle:=sourceoutput.Bundle{AuthorizedRecipient:id.Recipient().String(),Schema:1,Purpose:"dependency-lock-source-v1",Bind:bind,Manifests:manifests,Root:rootTool,Runtime:runtimeTool,Files:map[string][]byte{"package-lock.json":lock,"runtime/package-lock.json":lock},SHA256:map[string]string{"package-lock.json":sourceHash(lock),"runtime/package-lock.json":sourceHash(lock)}}
  manifestJSON,e:=json.Marshal(manifests);if e!=nil{t.Fatal(e)}
  p:=sourceoutput.Policy{Enabled:true,Purpose:bundle.Purpose,Bind:bind,ManifestSHA256:sourceHash(manifestJSON),Recipient:bundle.AuthorizedRecipient,Root:rootTool,Runtime:runtimeTool}
  selected:=id
  switch name{case "recipient-B-identity-A":p.Recipient=other.Recipient().String();case "recipient-and-identity-B":p.Recipient=other.Recipient().String();selected=other;case "empty-recipient":p.Recipient="";case "malformed-recipient":p.Recipient="malformed";case "noncanonical-recipient":p.Recipient+=" ";case "wrong-selected-identity":selected=other;case "replayed-attempt":bundle.Bind.Attempt=2}
  cipherDir:=filepath.Join(private,"cipher");if process.MakePrivateDir(cipherDir)!=nil{t.Fatal("cipher dir")}
  var cipher []byte
  if name=="forged-envelope-recipient"{
   bundle.AuthorizedRecipient=other.Recipient().String();var out bytes.Buffer
   w,e:=age.Encrypt(&out,id.Recipient());if e!=nil{t.Fatal(e)}
   if json.NewEncoder(w).Encode(bundle)!=nil||w.Close()!=nil{t.Fatal("forged synthetic envelope")};cipher=out.Bytes()
  }else{
   path,e:=sourceoutput.Seal(cipherDir,bundle,id.Recipient().String());if e!=nil{t.Fatal(e)}
   cipher,e=os.ReadFile(path);if e!=nil{t.Fatal(e)}
  }
  if name=="tampered-ciphertext"{cipher[len(cipher)-1]^=1}
  policy:=filepath.Join(private,"policy.json");policyJSON,e:=json.Marshal(p);if e!=nil{t.Fatal(e)};sourceWrite(t,policy,policyJSON)
  key:=filepath.Join(private,"synthetic-identity");sourceWrite(t,key,[]byte(selected.String()+"\n"))
  member:="locks-linux-debian-1.age";if name=="member-traversal"{member="../"+member}
  var archive bytes.Buffer;z:=zip.NewWriter(&archive);f,e:=z.Create(member);if e!=nil{t.Fatal(e)}
  if _,e=f.Write(cipher);e!=nil{t.Fatal(e)};if z.Close()!=nil{t.Fatal("zip")}
  run:=map[string]any{"id":4,"run_attempt":1,"workflow_id":3,"path":x.Registration.Caller.WorkflowPath,"head_branch":"main","head_sha":x.Registration.ControlSHA,"event":"workflow_dispatch","display_title":d.RunName(),"status":"completed","conclusion":"success","html_url":"https://github.com/example/checks/actions/runs/4","actor":map[string]any{"login":"example-admin"},"triggering_actor":map[string]any{"login":"example-admin"},"repository":map[string]any{"id":2,"full_name":"example/checks"}}
  if name=="wrong-provider-actor"{run["actor"]=map[string]any{"login":"not-authorized"}};if name=="wrong-provider-control"{run["head_sha"]=strings.Repeat("9",40)}
  jobs:=[]any{};for i,slot:=range []string{"preflight","plan","pack-linux-debian","collect"}{jobs=append(jobs,map[string]any{"id":i+1,"run_id":4,"run_attempt":1,"name":slot,"status":"completed","conclusion":"success","steps":[]any{map[string]any{"name":"Execute","conclusion":"success"}}})}
  artifact:=map[string]any{"id":99,"name":"source-locks-linux-debian-1","size_in_bytes":archive.Len(),"expired":name=="expired-artifact","workflow_run":map[string]any{"id":4,"head_sha":x.Registration.ControlSHA}}
  artifacts:=[]any{artifact};if name=="duplicate-artifact"{artifacts=append(artifacts,artifact)}
  apiReads,downloads:=0,0
  client,e:=gh.New("synthetic-fixture-token",time.Second,sourceRoundTrip(func(r *http.Request)(*http.Response,error){
   apiReads++;if r.Method!="GET"||!strings.Contains(r.Header.Get("Authorization"),"synthetic-fixture-token"){t.Fatal("API authentication contract")}
   switch{case strings.HasSuffix(r.URL.Path,"/jobs"):return sourceReply(r,map[string]any{"total_count":len(jobs),"jobs":jobs}),nil
   case strings.HasSuffix(r.URL.Path,"/artifacts/99/zip"):
    location:="https://results.blob.core.windows.net/synthetic";if name=="hostile-redirect"{location="https://example.com/untrusted"}
    return &http.Response{Request:r,StatusCode:302,Header:http.Header{"Location":[]string{location}},Body:io.NopCloser(strings.NewReader(""))},nil
   case strings.HasSuffix(r.URL.Path,"/artifacts"):return sourceReply(r,map[string]any{"total_count":len(artifacts),"artifacts":artifacts}),nil
   case strings.HasSuffix(r.URL.Path,"/runs/4"):return sourceReply(r,run),nil
   default:t.Fatal("unexpected retrieval API path");return nil,nil}
  }));if e!=nil{t.Fatal(e)}
  client.ArtifactTransport=sourceRoundTrip(func(r *http.Request)(*http.Response,error){downloads++;if r.Header.Get("Authorization")!=""||r.Header.Get("Cookie")!=""{t.Fatal("storage received API auth")};return &http.Response{Request:r,StatusCode:200,Header:http.Header{},Body:io.NopCloser(bytes.NewReader(archive.Bytes()))},nil})
  output:=filepath.Join(private,"returned-locks")
  if name=="existing-output"{if os.Mkdir(output,0700)!=nil{t.Fatal("existing output")};sourceWrite(t,filepath.Join(output,"keep"),[]byte("unchanged"))}
  if name=="parent-is-file"{parent:=filepath.Join(private,"parent-file");sourceWrite(t,parent,[]byte("ordinary file"));output=filepath.Join(parent,"returned")}
  if name=="public-environment"{t.Setenv("GITHUB_OUTPUT","synthetic-public-channel")}
  // Regular private files avoid pipe deadlock while capturing accidental output.
  stdout,e:=os.CreateTemp(private,"stdout-");if e!=nil{t.Fatal(e)}
  stderr,e:=os.CreateTemp(private,"stderr-");if e!=nil{t.Fatal(e)}
  oldOut,oldErr:=os.Stdout,os.Stderr
  var callErr error
  admitted:=false
  func(){os.Stdout,os.Stderr=stdout,stderr;defer func(){os.Stdout,os.Stderr=oldOut,oldErr}();a:=&app{};callErr=a.retrieveSourceOutput(context.Background(),client,x,policy,key,output);admitted=a.result.AdmissionEligible}()
  if stdout.Close()!=nil||stderr.Close()!=nil{t.Fatal("capture close")}
  if admitted{t.Fatal("source return admitted")}
  out,e:=os.ReadFile(stdout.Name());if e!=nil{t.Fatal(e)};errOut,e:=os.ReadFile(stderr.Name());if e!=nil{t.Fatal(e)}
  if len(out)!=0||len(errOut)!=0{t.Fatal("retrieval emitted output outside private result files")}
  if name=="positive"{
   if callErr!=nil{t.Fatal(callErr)}
   for _,path:=range []string{"package-lock.json","runtime/package-lock.json"}{v,e:=os.ReadFile(filepath.Join(output,path));if e!=nil||!bytes.Equal(v,lock)||!process.UserOnly(filepath.Join(output,path),false){t.Fatal("incorrect/nonprivate returned source")}}
   found:=map[string]bool{}
   e=filepath.WalkDir(output,func(path string,entry os.DirEntry,err error)error{if err!=nil{return err};if entry.IsDir(){return nil};rel,err:=filepath.Rel(output,path);if err!=nil{return err};rel=filepath.ToSlash(rel);if entry.Type()&os.ModeSymlink!=0||(rel!="package-lock.json"&&rel!="runtime/package-lock.json"){t.Fatal("nonallowlisted returned source")};found[rel]=true;return nil})
   if e!=nil||len(found)!=2{t.Fatal("return was not exactly two source files")}
  }else{
   if callErr==nil{t.Fatal("refused retrieval accepted")}
   if name=="existing-output"{v,e:=os.ReadFile(filepath.Join(output,"keep"));if e!=nil||string(v)!="unchanged"{t.Fatal("existing output overwritten")}}else if _,e:=os.Lstat(output);e==nil{t.Fatal("source output created on refusal")}
  }
  leftovers,e:=filepath.Glob(filepath.Join(private,".lock-source-*"));if e!=nil||len(leftovers)!=0{t.Fatal("private extraction staging retained")}
  if apiReads>5||downloads>1{t.Fatal("unbounded provider fixture")}
  if name=="positive"&&(apiReads!=5||downloads!=1){t.Fatal("positive did not traverse real provider artifact path")}
 })}
}
