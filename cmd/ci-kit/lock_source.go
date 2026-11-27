package main

import (
 "context"
 "os"
 "path/filepath"
 "strconv"

 "github.com/keeldock-platform/ci-kit/internal/catalog"
 "github.com/keeldock-platform/ci-kit/internal/config"
 "github.com/keeldock-platform/ci-kit/internal/process"
 "github.com/keeldock-platform/ci-kit/internal/sourceoutput"
)

// This branch is reached only AFTER readControl, signed source acquisition and
// clearing SOURCE_MAP/SOURCE_READ slots. The protocol grants no arbitrary args.
func lockSource(ctx context.Context,c control,root,private string) int {
 wd,e:=os.Getwd();if e!=nil{return 6}
 var gate struct { Enabled bool `json:"enabled"`; Purpose string `json:"purpose"` }
 if config.Read(filepath.Join(wd,"public-control",".github","source-output.json"),&gate,4096)!=nil || !gate.Enabled || gate.Purpose!="dependency-lock-source-v1" {return 3}
 // Exact per-candidate authorization lives in owner-maintained trusted CI
 // metadata, not in a source/kit self-referential commit. Nothing is inherited
 // from diagnostic recipient approval. This variable is stripped from children.
 var p sourceoutput.Policy
 if config.Decode([]byte(os.Getenv("CI_LOCK_SOURCE_POLICY")),&p,16384)!=nil || !p.Enabled || p.Purpose!=gate.Purpose{return 3}
 i:=c.Request.Inputs
 if p.Bind.SourceSHA!=i["source_sha"]||p.Bind.BaseSHA!=i["base_sha"] || p.Bind.KitSHA!=""||p.Bind.ControlSHA!=""||p.Bind.RunID!=""||p.Bind.RequestID!=""||p.Bind.RequestDigest!=""||p.Bind.Attempt!=0{return 3}
 attempt,e:=strconv.Atoi(os.Getenv("CI_ATTEMPT"));if e!=nil{return 3}
 actual:=sourceoutput.Bind{SourceSHA:i["source_sha"],BaseSHA:i["base_sha"],KitSHA:c.Policy.KitSHA,ControlSHA:c.Policy.ControlSHA,RequestID:i["request_id"],RequestDigest:i["request_digest"],RunID:os.Getenv("CI_RUN_ID"),Attempt:attempt}
 p.Bind=actual
 if sourceoutput.ValidateSource(root,p,actual)!=nil{return 3}
 if os.Getenv("CI_KIT_OPERATION")=="plan" {
  flags:=map[string]string{};for _,platform:=range catalog.Platforms{flags[platform]="false"};flags["linux-debian"]="true"
  if output(flags)!=nil{return 6};return 0
 }
 if os.Getenv("CI_KIT_OPERATION")!="pack"||os.Getenv("CI_PLATFORM")!="linux-debian"{return 3}
 // Relative staging identities are fixed, checked before path expansion. A
 // separately reviewed setup must provide these complete, hash-verified trees.
 if p.Root.Node!="root/bin/node"||p.Root.NpmRoot!="root/lib/node_modules/npm"||p.Runtime.Node!="runtime/bin/node"||p.Runtime.NpmRoot!="runtime/lib/node_modules/npm"{return 3}
 staging:=filepath.Join(os.Getenv("RUNNER_TEMP"),"ci-lock-tools")
 if _,e:=process.SafePath(staging,".");e!=nil{return 3}
 p.Root.Node=filepath.Join(staging,p.Root.Node);p.Root.NpmRoot=filepath.Join(staging,p.Root.NpmRoot)
 p.Runtime.Node=filepath.Join(staging,p.Runtime.Node);p.Runtime.NpmRoot=filepath.Join(staging,p.Runtime.NpmRoot)
 b,e:=sourceoutput.Generate(ctx,root,private,p,actual);if e!=nil{return 6}
 parent:=filepath.Join(os.Getenv("RUNNER_TEMP"),"ci-source-output")
 if process.MakePrivateDir(parent)!=nil{return 6}
 dir:=filepath.Join(parent,strconv.Itoa(attempt));if os.Mkdir(dir,0700)!=nil{return 6}
 sealed,e:=sourceoutput.Seal(dir,b,p.Recipient);if e!=nil{return 6}
 target:=filepath.Join(dir,"locks-linux-debian-"+strconv.Itoa(attempt)+".age")
 if os.Link(sealed,target)!=nil{return 6};if os.Remove(sealed)!=nil{return 6}
 // The outer privateWork defers close acquisition, remove plaintext, then emit
 // the fixed readiness flag. Upload is gated on success AND that flag.
 return 0
}
