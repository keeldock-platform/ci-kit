package main

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io/fs"
 "os"
 "path/filepath"
 "reflect"
 "strconv"
 "strings"
 "time"

 "github.com/keeldock-platform/ci-kit/internal/config"
 "github.com/keeldock-platform/ci-kit/internal/process"
 "github.com/keeldock-platform/ci-kit/internal/protocol"
)
const generatedActionLock=".github/workflows/actions.lock"
const lockInputTree="1e105ee2be7453239791d596fa89a9c42598f4dc"
type lockOperation struct { Schema int `json:"schema_version"`; InputCommit string `json:"input_commit"`; InputTree string `json:"input_tree"`; Base string `json:"base"`; InputLockSHA256 string `json:"input_lock_sha256"` }
func actionDigest(raw []byte)string{sum:=sha256.Sum256(raw);return hex.EncodeToString(sum[:])}
func actionSnapshot(root string)(map[string]string,error){
 result:=map[string]string{};total:=int64(0)
 e:=filepath.WalkDir(root,func(path string,d fs.DirEntry,err error)error{
  if err!=nil{return refused};rel,e:=filepath.Rel(root,path);if e!=nil{return refused};if rel=="."{return nil};if rel==".git"&&d.IsDir(){return filepath.SkipDir}
  if d.Type()&os.ModeSymlink!=0{return refused};if d.IsDir(){return nil}
  st,e:=d.Info();if e!=nil||!st.Mode().IsRegular()||st.Size()>16<<20{return refused};total+=st.Size();if total>128<<20||len(result)>=10000{return refused}
  raw,e:=os.ReadFile(path);if e!=nil{return refused};result[filepath.ToSlash(rel)]=actionDigest(raw)+":"+strconv.FormatUint(uint64(st.Mode().Perm()),8);return nil
 });return result,e
}
func onlyActionLock(before,after map[string]string)bool{
 if len(before)!=len(after)||before[generatedActionLock]==""||after[generatedActionLock]==""||before[generatedActionLock]==after[generatedActionLock]{return false}
 a:=map[string]string{};b:=map[string]string{};for p,v:=range before{if p!=generatedActionLock{a[p]=v}};for p,v:=range after{if p!=generatedActionLock{b[p]=v}};return reflect.DeepEqual(a,b)
}
func generateActionLock(ctx context.Context)(code int){
 if os.Getenv("CI")!="true"||os.Getenv("GITHUB_REPOSITORY")!="keeldock-platform/ci-kit"||os.Getenv("GITHUB_EVENT_NAME")!="workflow_dispatch"||os.Getenv("GITHUB_REF")!="refs/heads/ci/action-lock-generation-1127"{return 3}
 control,e:=os.Getwd();if e!=nil{return 6}
 var op lockOperation
 if config.Read(filepath.Join(control,"docs","action-lock-operation.json"),&op,4096)!=nil||op.Schema!=1||!protocol.SHA(op.InputCommit)||op.InputTree!=lockInputTree||op.Base!="b59374a9976d240b03387eaac7d91ca4a5697ef5"||!protocol.Digest(op.InputLockSHA256){return 3}
 parent:=os.Getenv("RUNNER_TEMP");if _,e:=process.SafePath(parent,".");e!=nil{return 3}
 private,e:=os.MkdirTemp(parent,"action-lock-private-");if e!=nil{return 6}
 defer func(){if os.RemoveAll(private)!=nil&&code==0{code=6}}()
 target:=filepath.Join(private,"target");index:=0
 call:=func(root string,argv []string,auth bool)([]byte,error){
  index++;spec:=process.Spec{Argv:argv,Root:root,Cwd:".",ResultDir:filepath.Join(private,"capture-"+strconv.Itoa(index)),Timeout:2*time.Minute,OutputLimit:2<<20,Env:map[string]string{"HOME":private,"PATH":"/usr/bin:/bin","CI":"true"}}
  var r process.Result;var err error
  if auth{r,err=process.RunPublicActionLock(ctx,spec,os.Getenv("GH_TOKEN"))}else{r,err=process.Run(ctx,spec)}
  if err!=nil||r.ExitCode!=0||!r.CleanupComplete||r.TimedOut||r.Overflow{return nil,refused}
  return readRegular(r.StdoutPath,2<<20)
 }
 controlHead,e:=call(control,[]string{"/usr/bin/git","rev-parse","HEAD"},false);if e!=nil||!protocol.SHA(os.Getenv("GITHUB_SHA"))||strings.TrimSpace(string(controlHead))!=os.Getenv("GITHUB_SHA"){return 3}
 if _,e:=call(control,[]string{"/usr/bin/git","clone","--no-checkout","--depth=1","https://github.com/keeldock-platform/ci-kit.git",target},false);e!=nil{return 6}
 current,e:=call(target,[]string{"/usr/bin/git","rev-parse","origin/main"},false);if e!=nil||strings.TrimSpace(string(current))!=op.Base{return 3}
 if _,e:=call(target,[]string{"/usr/bin/git","fetch","--depth=1","origin",op.InputCommit},false);e!=nil{return 6}
 if _,e:=call(target,[]string{"/usr/bin/git","-c","core.hooksPath=/dev/null","checkout","--detach",op.InputCommit},false);e!=nil{return 6}
 head,e:=call(target,[]string{"/usr/bin/git","rev-parse","HEAD"},false);if e!=nil||strings.TrimSpace(string(head))!=op.InputCommit{return 6}
 tree,e:=call(target,[]string{"/usr/bin/git","rev-parse","HEAD^{tree}"},false);if e!=nil||strings.TrimSpace(string(tree))!=op.InputTree{return 6}
 old,e:=readRegular(filepath.Join(target,generatedActionLock),2<<20);if e!=nil||actionDigest(old)!=op.InputLockSHA256{return 6}
 before,e:=actionSnapshot(target);if e!=nil{return 6}
 tool:=filepath.Join(parent,"policy-tools","gh-actions-lock")
 if _,e=call(target,[]string{tool,"--no-narrow"},true);e!=nil{return 6}
 after,e:=actionSnapshot(target);if e!=nil||!onlyActionLock(before,after){return 6}
 if _,e=call(target,[]string{tool,"--verify","--fail-on-warning"},true);e!=nil{return 6}
 end,e:=actionSnapshot(target);if e!=nil||!reflect.DeepEqual(after,end){return 6}
 raw,e:=readRegular(filepath.Join(target,generatedActionLock),2<<20);if e!=nil||len(raw)==0{return 6}
 // Only the genuine generated lock is returned. No source archive/diff/log,
 // command, environment, receipt assertion or automatic commit is published.
 output:=filepath.Join(parent,"generated-action-lock");if process.MakePrivateDir(output)!=nil{return 6}
 f,e:=process.CreatePrivateFile(filepath.Join(output,"actions.lock"));if e!=nil{return 6}
 n,e:=f.Write(raw);ce:=f.Close();if e!=nil||ce!=nil||n!=len(raw){return 6}
 return 0
}
// The existing main prints only fixed PASS/FAIL. This operation is generation,
// never whole-kit policy, person approval, release or admission evidence.
