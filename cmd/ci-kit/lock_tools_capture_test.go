//go:build linux

package main

// Actual source-free timeout/cancel/descendant fixtures under the existing
// process owner. AUTHORED / NOT RUN; no Node/npm/downloaded code or network.
import (
 "context"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "syscall"
 "testing"
 "time"

 "github.com/keeldock-platform/ci-kit/internal/process"
)
func TestLockToolsBoundedCaptureUsesExistingOwnerCleanup(t *testing.T){
 root,e:=filepath.Abs("../..");if e!=nil{t.Fatal(e)}
 root,e=filepath.EvalSymlinks(root);if e!=nil{t.Fatal(e)}
 script:=filepath.Join(root,"packs","linux-debian","lock-source-tools","capture_fixture.py")
 for _,mode:=range []string{"timeout","cancel","descendant-overflow"}{t.Run(mode,func(t *testing.T){
  private,e:=filepath.EvalSymlinks(t.TempDir());if e!=nil{t.Fatal(e)}
  work:=filepath.Join(private,"work");if process.MakePrivateDir(work)!=nil{t.Fatal("work")}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  done:=make(chan process.Result,1)
  go func(){result,_:=process.Run(ctx,process.Spec{Root:root,Cwd:".",Argv:[]string{"/usr/bin/python3","-I","-B",script,mode,work},ResultDir:filepath.Join(private,"capture"),Timeout:3*time.Second,Grace:200*time.Millisecond,ReapTimeout:3*time.Second,OutputLimit:65536,Env:map[string]string{"HOME":work,"PATH":"/usr/bin:/bin","CI":"true"}});done<-result}()
  // Bounded test synchronization, not a process/deadline owner or CI poll loop.
  ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
  readyDeadline:=time.NewTimer(2*time.Second);defer readyDeadline.Stop()
  var pid int
 ready:
  for{select{
   case <-ticker.C:
    raw,err:=os.ReadFile(filepath.Join(work,"child.pid"));if err==nil{pid,err=strconv.Atoi(strings.TrimSpace(string(raw)));if err==nil&&pid>0{break ready}}
   case <-readyDeadline.C:cancel();select{case <-done:case <-time.After(40*time.Second):t.Fatal("existing owner failed to settle after missing readiness")};t.Fatal("fixture never established a real child PID")
  }}
  if mode=="cancel"{cancel()}
  var result process.Result
  select{case result=<-done:case <-time.After(40*time.Second):cancel();t.Fatal("existing owner did not settle within its bounded cleanup budget")}
  if !result.CleanupComplete{t.Fatal("descendant cleanup was not confirmed")}
  if mode=="timeout"&&!result.TimedOut{t.Fatal("timeout identity lost")}
  if mode=="cancel"&&!result.Interrupted{t.Fatal("cancel identity lost")}
  if mode=="descendant-overflow"&&result.ExitCode!=6{t.Fatal("overflow refusal lost")}
  if syscall.Kill(pid,0)!=syscall.ESRCH{t.Fatal("actual fixture child/descendant still exists")}
  raw,e:=os.ReadFile(result.StderrPath);if e!=nil{t.Fatal(e)}
  if mode=="descendant-overflow"&&!strings.Contains(string(raw),"overflow_stream"){t.Fatal("overflow structured record absent")}
 })}
}
