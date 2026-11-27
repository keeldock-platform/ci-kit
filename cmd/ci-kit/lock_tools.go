package main

import (
 "context"
 "encoding/json"
 "os"
 "path/filepath"
 "runtime"
 "time"

 "github.com/keeldock-platform/ci-kit/internal/process"
 "github.com/keeldock-platform/ci-kit/internal/protocol"
)

// Source-free bootstrap companion only. There is no source acquisition, secret
// mapping, arbitrary executable, output path, version or command input here.
func lockTools(ctx context.Context,mode string)(code int){
 if mode!="qualify"&&mode!="stage"||runtime.GOOS!="linux"||runtime.GOARCH!="amd64"||os.Getenv("CI")!="true"{return 3}
 if !protocol.SHA(os.Getenv("CI_KIT_SHA")){return 3}
 if mode=="qualify"&&(os.Getenv("GITHUB_REPOSITORY")!="keeldock-platform/ci-kit"||os.Getenv("GITHUB_EVENT_NAME")!="workflow_dispatch"||os.Getenv("GITHUB_REF")!="refs/heads/main"||!protocol.Numeric(os.Getenv("GITHUB_RUN_ID"))||!protocol.Numeric(os.Getenv("GITHUB_RUN_ATTEMPT"))){return 3}
 if mode=="stage"&&(os.Getenv("CI_PLATFORM")!="linux-debian"||os.Getenv("CI_KIT_OPERATION")!="pack"){return 3}
 parent:=os.Getenv("RUNNER_TEMP");if !filepath.IsAbs(parent){return 3}
 if _,e:=process.SafePath(parent,".");e!=nil{return 3}
 wd,e:=os.Getwd();if e!=nil{return 6}
 root:=filepath.Join(wd,"public-control");if _,e:=process.SafePath(root,".");e!=nil{return 3}
 private,e:=os.MkdirTemp(parent,"ci-lock-stage-");if e!=nil{return 6}
 defer func(){if os.RemoveAll(private)!=nil&&code==0{code=6}}()
 result,e:=process.Run(ctx,process.Spec{Argv:[]string{"/usr/bin/python3","-I","-B",filepath.Join(root,"packs","linux-debian","lock-source-tools","stage.py"),mode,private,parent},Root:root,Cwd:".",ResultDir:filepath.Join(private,"capture"),Timeout:10*time.Minute,OutputLimit:1<<20,Env:map[string]string{"HOME":private,"USERPROFILE":private,"PATH":"/usr/bin:/bin","CI":"true"}})
 if !result.CleanupComplete{return 6}
 if result.TimedOut{return 4}
 if state:=contextExit(ctx);state!=0{return state}
 if e!=nil||result.ExitCode!=0||result.Overflow{return 6}
 if mode=="qualify"{
  raw,e:=readRegular(filepath.Join(private,"qualification.json"),65536);if e!=nil{return 6}
  var receipt map[string]any;if json.Unmarshal(raw,&receipt)!=nil{return 6}
  receipt["provider_binding"]=map[string]string{"run_id":os.Getenv("GITHUB_RUN_ID"),"attempt":os.Getenv("GITHUB_RUN_ATTEMPT"),"kit_sha":os.Getenv("CI_KIT_SHA"),"repository":os.Getenv("GITHUB_REPOSITORY")}
  raw,e=json.MarshalIndent(receipt,"","  ");if e!=nil{return 6}
  f,e:=process.CreatePrivateFile(filepath.Join(parent,"ci-lock-tools-qualification.json"));if e!=nil{return 6}
  n,e:=f.Write(append(raw,'\n'));ce:=f.Close();if e!=nil||ce!=nil||n!=len(raw)+1{return 6}
 }
 return 0
}
