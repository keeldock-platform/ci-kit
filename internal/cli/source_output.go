package cli

import (
 "bytes"
 "context"
 "os"
 "path/filepath"
 "strconv"

 "filippo.io/age"
 "github.com/keeldock-platform/ci-kit/internal/config"
 gh "github.com/keeldock-platform/ci-kit/internal/github"
 "github.com/keeldock-platform/ci-kit/internal/process"
 "github.com/keeldock-platform/ci-kit/internal/sourceoutput"
)

// Owner-local retrieval requires an existing identity and existing private policy.
// There is no identity creation, CI identity access, plaintext stdout or public
// artifact fallback. Generic remote() first verifies current route/registration,
// saved composition/expectation, actor and attempt against provider metadata.
func (a *app) retrieveSourceOutput(ctx context.Context,client *gh.Client,x gh.Expectation,policy,key,output string) error {
 if !diagnosticsLocal()||policy==""||output==""||!process.UserOnly(policy,false){return gh.Fail(3,"policy_refused")}
 var p sourceoutput.Policy
 if config.Read(policy,&p,16384)!=nil||!p.Enabled||p.Bind.SourceSHA!=x.Dispatch.Inputs["source_sha"]||p.Bind.BaseSHA!=x.Dispatch.Inputs["base_sha"]{return gh.Fail(3,"policy_refused")}
 p.Bind=sourceoutput.Bind{SourceSHA:x.Dispatch.Inputs["source_sha"],BaseSHA:x.Dispatch.Inputs["base_sha"],KitSHA:x.Registration.KitSHA,ControlSHA:x.Registration.ControlSHA,RequestID:x.Dispatch.Inputs["request_id"],RequestDigest:x.Dispatch.Inputs["request_digest"],RunID:x.RunID,Attempt:x.Attempt}
 key=diagnosticKeyPath(key)
 if !process.UserOnly(key,false)||!config.NoLinks(key){return gh.Fail(3,"policy_refused")}
 st,e:=os.Stat(key);if e!=nil||!st.Mode().IsRegular()||st.Size()>65536{return gh.Fail(3,"policy_refused")}
 raw,e:=os.ReadFile(key);if e!=nil{return gh.Fail(6,"missing_evidence")}
 ids,e:=age.ParseIdentities(bytes.NewReader(raw));if e!=nil||len(ids)!=1{return gh.Fail(6,"missing_evidence")}
 if sourceoutput.ValidateRecipientIdentity(p.Recipient,ids[0])!=nil{return gh.Fail(3,"policy_refused")}
 output,e=filepath.Abs(output);if e!=nil||!config.NoLinks(filepath.Dir(output))||!process.UserOnly(filepath.Dir(output),true){return gh.Fail(3,"policy_refused")}
 if _,e=os.Lstat(output);!os.IsNotExist(e){return gh.Fail(3,"policy_refused")}
 var bundle sourceoutput.Bundle
 seen:=false
 e=client.WalkSourceOutput(ctx,x,func(slot string,cipher []byte) error {
  if seen||slot!="linux-debian"{return gh.Fail(6,"missing_evidence")};seen=true
  var err error;bundle,err=sourceoutput.Open(bytes.NewReader(cipher),ids[0],p)
  if err!=nil{return gh.Fail(6,"missing_evidence")};return nil
 })
 if e!=nil{return e};if !seen{return gh.Fail(6,"missing_evidence")}
 // The two files are extracted only after complete receipt/envelope validation.
 // The user-only parent is exclusively owned for the operation; no overwrites.
 stage,e:=os.MkdirTemp(filepath.Dir(output),".lock-source-"+strconv.Itoa(x.Attempt)+"-");if e!=nil{return gh.Fail(6,"missing_evidence")}
 defer os.RemoveAll(stage)
 if os.Mkdir(filepath.Join(stage,"runtime"),0700)!=nil{return gh.Fail(6,"missing_evidence")}
 for _,name:=range []string{"package-lock.json","runtime/package-lock.json"}{
  f,err:=os.OpenFile(filepath.Join(stage,name),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if err!=nil{return gh.Fail(6,"missing_evidence")}
  n,err:=f.Write(bundle.Files[name]);ce:=f.Close();if err!=nil||ce!=nil||n!=len(bundle.Files[name]){return gh.Fail(6,"missing_evidence")}
 }
 if _,e=os.Lstat(output);!os.IsNotExist(e){return gh.Fail(3,"policy_refused")}
 if os.Rename(stage,output)!=nil{return gh.Fail(6,"missing_evidence")}
 a.result.AdmissionEligible=false
 return nil
}
