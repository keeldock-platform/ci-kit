package github

// Actual source-free provider method fixtures with in-memory HTTP transports.
// No network, source checkout, tool install, real token or age identity is used.
// AUTHORED / NOT RUN in this preparation task.
import (
 "archive/zip"
 "bytes"
 "context"
 "io"
 "net/http"
 "strings"
 "testing"
 "time"

 "github.com/keeldock-platform/ci-kit/internal/protocol"
)

func sourceExpectation(t *testing.T) Expectation {
 t.Helper();x:=fixture();x.Registration.Concern="core";x.Registration.Platforms=[]string{"linux-debian"}
 d,e:=protocol.NewDispatch("core",strings.Repeat("a",40),strings.Repeat("b",40),"unit","source-output","linux-debian","all","11111111-1111-4111-8111-111111111111")
 if e!=nil{t.Fatal(e)};x.Dispatch=d;return x
}
func TestSourceOutputProviderAuthenticationArtifactAndRedirect(t *testing.T) {
 cases:=[]string{"positive","run-id","run-attempt","control-head","request-title","actor","workflow","missing-finalizer","skipped-producer","wrong-job-attempt","duplicate-job","missing-artifact","duplicate-artifact","truncated-inventory","expired-artifact","artifact-run","artifact-head","artifact-attempt-name","oversized-artifact","member-traversal","wrong-member","two-members","corrupt-zip","hostile-redirect","redirect-userinfo","redirect-port","redirect-http","second-redirect"}
 for _,name:=range cases{t.Run(name,func(t *testing.T){
  x:=sourceExpectation(t);run:=fixtureRun(x)
  switch name{case "run-id":run.ID=5;case "run-attempt":run.RunAttempt=2;case "control-head":run.HeadSHA=strings.Repeat("9",40);case "request-title":run.DisplayTitle="wrong request";case "actor":run.Actor.Login="unapproved";case "workflow":run.WorkflowID=99}
  jobs:=[]map[string]any{}
  for i,slot:=range []string{"preflight","plan","pack-linux-debian","collect"}{jobs=append(jobs,map[string]any{"id":i+1,"run_id":4,"run_attempt":1,"name":slot,"status":"completed","conclusion":"success","steps":[]any{map[string]any{"name":"Execute","conclusion":"success"}}})}
  switch name{case "missing-finalizer":jobs=jobs[:3];case "skipped-producer":jobs[2]["conclusion"]="skipped";case "wrong-job-attempt":jobs[2]["run_attempt"]=2;case "duplicate-job":jobs=append(jobs,jobs[2])}
  member:="locks-linux-debian-1.age";if name=="member-traversal"{member="../"+member};if name=="wrong-member"{member="failure-linux-debian-1.age"}
  var archive bytes.Buffer;z:=zip.NewWriter(&archive);f,e:=z.Create(member);if e!=nil{t.Fatal(e)}
  if _,e=f.Write([]byte("synthetic ciphertext marker"));e!=nil{t.Fatal(e)}
  if name=="two-members"{if _,e=z.Create("extra.age");e!=nil{t.Fatal(e)}}
  if z.Close()!=nil{t.Fatal("zip fixture")};zipBytes:=archive.Bytes();if name=="corrupt-zip"{zipBytes=[]byte("not a zip")}
  artifactRun:=map[string]any{"id":4,"head_sha":x.Registration.ControlSHA}
  artifact:=map[string]any{"id":99,"name":"source-locks-linux-debian-1","size_in_bytes":len(zipBytes),"expired":false,"workflow_run":artifactRun}
  switch name{case "expired-artifact":artifact["expired"]=true;case "artifact-run":artifactRun["id"]=5;case "artifact-head":artifactRun["head_sha"]=strings.Repeat("9",40);case "artifact-attempt-name":artifact["name"]="source-locks-linux-debian-2";case "oversized-artifact":artifact["size_in_bytes"]=maxDiagnosticDownload+1}
  artifacts:=[]any{artifact};if name=="missing-artifact"{artifacts=nil};if name=="duplicate-artifact"{artifacts=append(artifacts,artifact)}
  total:=len(artifacts);if name=="truncated-inventory"{total++}
  location:="https://results.blob.core.windows.net/file?signed=synthetic"
  switch name{case "hostile-redirect":location="https://example.com/file";case "redirect-userinfo":location="https://user@results.blob.core.windows.net/file";case "redirect-port":location="https://results.blob.core.windows.net:444/file";case "redirect-http":location="http://results.blob.core.windows.net/file"}
  apiReads,downloads,visits:=0,0,0
  client,e:=New("synthetic-source-readback-token",time.Second,transportFunc(func(r *http.Request)(*http.Response,error){
   apiReads++;if r.Method!="GET"||!strings.Contains(r.Header.Get("Authorization"),"synthetic-source-readback-token"){t.Fatal("API auth/method contract")}
   switch{case strings.HasSuffix(r.URL.Path,"/jobs"):
    if !strings.Contains(r.URL.Path,"/attempts/1/"){t.Fatal("attempt not pinned")};return reply(200,map[string]any{"total_count":len(jobs),"jobs":jobs}),nil
   case strings.HasSuffix(r.URL.Path,"/artifacts/99/zip"):
    return &http.Response{Request:r,StatusCode:302,Header:http.Header{"Location":[]string{location}},Body:io.NopCloser(strings.NewReader(""))},nil
   case strings.HasSuffix(r.URL.Path,"/artifacts"):return reply(200,map[string]any{"total_count":total,"artifacts":artifacts}),nil
   case strings.HasSuffix(r.URL.Path,"/runs/4"):return reply(200,run),nil
   default:t.Fatal("unexpected API path");return nil,nil}
  }));if e!=nil{t.Fatal(e)}
  client.ArtifactTransport=transportFunc(func(r *http.Request)(*http.Response,error){
   downloads++;if r.Header.Get("Authorization")!=""||r.Header.Get("Cookie")!=""{t.Fatal("API credential forwarded to storage")}
   code:=200;headers:=http.Header{};if name=="second-redirect"{code=302;headers.Set("Location","https://other.blob.core.windows.net/again")}
   return &http.Response{Request:r,StatusCode:code,Header:headers,Body:io.NopCloser(bytes.NewReader(zipBytes))},nil
  })
  e=client.WalkSourceOutput(context.Background(),x,func(slot string,cipher []byte)error{
   visits++;if slot!="linux-debian"||string(cipher)!="synthetic ciphertext marker"{t.Fatal("wrong artifact delivered")};return nil
  })
  if name=="positive"{if e!=nil||visits!=1||downloads!=1||apiReads!=5{t.Fatalf("positive provider fixture failed: %v reads=%d downloads=%d visits=%d",e,apiReads,downloads,visits)}}else if e==nil{t.Fatal("unbound provider/artifact evidence accepted")}
  if downloads>1||apiReads>5{t.Fatal("unexpected retry, extra redirect or unbounded fixture request")}
 })}
}
