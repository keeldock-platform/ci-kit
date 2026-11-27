package process

import (
 "context"
 "os"
 "path/filepath"
 "testing"
)
// Source-free refusal source only; no generator/token/network is exercised.
func TestPublicActionLockBridgeRefusesUnreviewedInputs(t *testing.T){
 root,e:=filepath.EvalSymlinks(t.TempDir());if e!=nil{t.Fatal(e)}
 source:=filepath.Join(root,"source");if os.Mkdir(source,0700)!=nil{t.Fatal("source fixture")}
 tool:=filepath.Join(root,"not-a-tool");if os.WriteFile(tool,[]byte("inert synthetic bytes, never executed"),0600)!=nil{t.Fatal("fixture")}
 for _,c:=range []struct{name,token string;args []string}{
  {"no-token","",[]string{tool,"--no-narrow"}},
  {"token-newline","synthetic\n",[]string{tool,"--no-narrow"}},
  {"arbitrary-arguments","synthetic-fixture",[]string{tool,"--accept-moved"}},
  {"unreviewed-binary","synthetic-fixture",[]string{tool,"--no-narrow"}},
  {"wrong-verification-mode","synthetic-fixture",[]string{tool,"--verify","--ignore-local-actions"}},
 }{t.Run(c.name,func(t *testing.T){
  resultDir:=filepath.Join(root,"must-not-run-"+c.name)
  _,err:=RunPublicActionLock(context.Background(),Spec{Argv:c.args,Root:source,Cwd:".",ResultDir:resultDir},c.token)
  if err==nil{t.Fatal("unreviewed auth/tool execution admitted")}
  if _,e:=os.Lstat(resultDir);!os.IsNotExist(e){t.Fatal("refused request started capture/execution")}
 })}
}
