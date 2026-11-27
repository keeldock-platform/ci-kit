package main

import (
 "encoding/json"
 "errors"
 "io"
 "os"
 "path/filepath"
 "runtime"
 "testing"
)
func env(m map[string]string) func(string)string{return func(k string)string{return m[k]}}
func baseEnv()map[string]string{return map[string]string{"CI":"true","CI_KIT_SHA":"0123456789abcdef0123456789abcdef01234567","GITHUB_REPOSITORY":"keeldock-platform/ci-kit","GITHUB_EVENT_NAME":"workflow_dispatch","GITHUB_REF":"refs/heads/ci/action-lock-generation-1127","GITHUB_RUN_ID":"12345","GITHUB_RUN_ATTEMPT":"1","CI_PLATFORM":"linux-debian","CI_KIT_OPERATION":"lock-qualification"}}
func TestLockToolsCandidateRouteGuard(t *testing.T){
 if runtime.GOOS!="linux"||runtime.GOARCH!="amd64"{t.Skip("Linux amd64 positive route")};m:=baseEnv();if !lockToolsModeAllowed("qualify-candidate","linux","amd64",env(m)){t.Fatal("candidate route refused")}
 for _,k:=range []string{"GITHUB_REPOSITORY","GITHUB_EVENT_NAME","GITHUB_REF","CI","CI_KIT_SHA","GITHUB_RUN_ID","GITHUB_RUN_ATTEMPT","CI_PLATFORM","CI_KIT_OPERATION"}{t.Run(k,func(t *testing.T){x:=baseEnv();x[k]="bad";if lockToolsModeAllowed("qualify-candidate","linux","amd64",env(x)){t.Fatalf("accepted bad %s",k)}})}
 x:=baseEnv();x["CI_KIT_SHA"]="not-sha";if lockToolsModeAllowed("qualify-candidate","linux","amd64",env(x)){t.Fatal("malformed SHA accepted")};x=baseEnv();x["GITHUB_RUN_ATTEMPT"]="0";if lockToolsModeAllowed("qualify-candidate","linux","amd64",env(x)){t.Fatal("zero attempt accepted")}
 if lockToolsModeAllowed("qualify-candidate","darwin","amd64",env(baseEnv()))||lockToolsModeAllowed("qualify-candidate","linux","arm64",env(baseEnv())){t.Fatal("wrong host accepted")}
}
func TestLockToolsNormalRoutes(t *testing.T){
 m:=baseEnv();m["GITHUB_REF"]="refs/heads/main";if !lockToolsModeAllowed("qualify","linux","amd64",env(m)){t.Fatal("main qualify refused")};m["GITHUB_REF"]="refs/heads/ci/action-lock-generation-1127";if lockToolsModeAllowed("qualify","linux","amd64",env(m)){t.Fatal("qualify accepted candidate ref")};m["CI_PLATFORM"]="linux-debian";m["CI_KIT_OPERATION"]="pack";if !lockToolsModeAllowed("stage","linux","amd64",env(m)){t.Fatal("pack staging refused")};m["CI_PLATFORM"]="darwin";if lockToolsModeAllowed("stage","linux","amd64",env(m)){t.Fatal("stage accepted wrong platform")};m["CI_PLATFORM"]="linux-debian";m["CI_KIT_OPERATION"]="plan";if lockToolsModeAllowed("stage","linux","amd64",env(m)){t.Fatal("stage accepted non-pack")};if lockToolsModeAllowed("bogus","linux","amd64",env(m)){t.Fatal("unknown mode accepted")};m=baseEnv();m["CI"]="false";if lockToolsModeAllowed("stage","linux","amd64",env(m)){t.Fatal("non-CI accepted")}
}
type testReceiptFile struct{file *os.File; short bool; writeErr,closeErr error; closed bool}
func(f *testReceiptFile)Write(b []byte)(int,error){if f.writeErr!=nil{_,_=f.file.Write(b[:len(b)/2]);return 0,f.writeErr};if f.short{_,_=f.file.Write(b[:len(b)/2]);return len(b)/2,nil};return f.file.Write(b)}
func(f *testReceiptFile)Close()error{f.closed=true;if f.closeErr!=nil{_ = f.file.Close();return f.closeErr};return f.file.Close()}
func receiptCreate(path string,short bool,writeErr,closeErr error)(receiptFile,error){f,e:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600);if e!=nil{return nil,e};return &testReceiptFile{file:f,short:short,writeErr:writeErr,closeErr:closeErr},nil}
func TestCandidateReceiptWriteContract(t *testing.T){
 dir:=t.TempDir();mainPath:=filepath.Join(dir,qualificationRecordName("qualify"));if err:=os.WriteFile(mainPath,[]byte("main-receipt"),0600);err!=nil{t.Fatal(err)}
 raw:=[]byte(`{"schema_version":1,"approved":true,"source":"fixture"}`);m:=baseEnv();m["GITHUB_RUN_ID"]="8";m["GITHUB_RUN_ATTEMPT"]="2";m["CI_KIT_SHA"]="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 create:=func(path string)(receiptFile,error){return receiptCreate(path,false,nil,nil)}
 if err:=writeQualificationRecord(dir,"qualify-candidate",raw,env(m),create,os.Remove);err!=nil{t.Fatal(err)}
 candidatePath:=filepath.Join(dir,"ci-lock-tools-candidate-qualification.json");got,err:=os.ReadFile(candidatePath);if err!=nil{t.Fatal(err)};var value map[string]any;if json.Unmarshal(got,&value)!=nil{t.Fatal("invalid candidate JSON")}
 if value["qualification_class"]!="candidate-only"||value["approved"]!=false||value["source"]!="fixture"{t.Fatalf("wrong candidate fields: %#v",value)};binding,ok:=value["provider_binding"].(map[string]any);if !ok||binding["run_id"]!="8"||binding["attempt"]!="2"||binding["kit_sha"]!=m["CI_KIT_SHA"]||binding["repository"]!=m["GITHUB_REPOSITORY"]{t.Fatalf("wrong runtime binding: %#v",value["provider_binding"])}
 main,err:=os.ReadFile(mainPath);if err!=nil||string(main)!="main-receipt"{t.Fatal("main receipt changed")};if err=writeQualificationRecord(dir,"qualify-candidate",raw,env(m),create,os.Remove);err==nil{t.Fatal("candidate collision accepted")};after,_:=os.ReadFile(candidatePath);if string(after)!=string(got){t.Fatal("collision changed existing candidate")}
 mainDir:=t.TempDir();mainOutput:=filepath.Join(mainDir,qualificationRecordName("qualify"));if err=writeQualificationRecord(mainDir,"qualify",raw,env(m),create,os.Remove);err!=nil{t.Fatal(err)};mainValueRaw,readErr:=os.ReadFile(mainOutput);var mainValue map[string]any;if readErr!=nil||json.Unmarshal(mainValueRaw,&mainValue)!=nil||mainValue["source"]!="fixture"||mainValue["qualification_class"]!=nil||mainValue["approved"]!=true{t.Fatalf("main qualification semantics changed: %#v",mainValue)}
}
func TestCandidateReceiptWriteFailuresRemovePartialFile(t *testing.T){
 boom:=errors.New("write failure");for _,tc:=range []struct{name string;short bool;writeErr,closeErr error}{{"write",false,boom,nil},{"short",true,nil,nil},{"close",false,nil,boom}}{t.Run(tc.name,func(t *testing.T){dir:=t.TempDir();path:=filepath.Join(dir,"ci-lock-tools-candidate-qualification.json");var opened *testReceiptFile;create:=func(p string)(receiptFile,error){f,e:=receiptCreate(p,tc.short,tc.writeErr,tc.closeErr);if e==nil{opened=f.(*testReceiptFile)};return f,e};err:=writeQualificationRecord(dir,"qualify-candidate",[]byte(`{"schema_version":1}`),env(baseEnv()),create,os.Remove);if err==nil||opened==nil||!opened.closed{t.Fatal("failure or close was not observed")};if _,e:=os.Stat(path);!os.IsNotExist(e){t.Fatal("partial output was not removed")}}) }
 dir:=t.TempDir();if writeQualificationRecord(dir,"qualify-candidate",[]byte(`{"x":1}`),env(baseEnv()),func(string)(receiptFile,error){return nil,io.ErrClosedPipe},os.Remove)==nil{t.Fatal("open refusal accepted")}
}
