package sourceoutput

// Source-free, AUTHORED / NOT RUN. Tests the actual existing tool verifier and
// the exact map encoding used by the proposed Python stager. Synthetic bytes
// below are not an official Node/npm payload or a staging receipt.
import (
 "encoding/json"
 "os"
 "path/filepath"
 "testing"
)
func TestStagingTreeDigestMatchesToolVerifier(t *testing.T){
 root,e:=filepath.EvalSymlinks(t.TempDir());if e!=nil{t.Fatal(e)}
 source:=filepath.Join(root,"source");if os.Mkdir(source,0700)!=nil{t.Fatal("source fixture")}
 stage:=filepath.Join(root,"staged");node:=filepath.Join(stage,"root","bin","node");npm:=filepath.Join(stage,"root","lib","node_modules","npm")
 files:=map[string][]byte{"package.json":[]byte(`{"name":"npm","version":"12.2.2"}`),"bin/npm-cli.js":[]byte("synthetic never executed"),"node_modules/synthetic/package.json":[]byte("{}")}
 hashes:=map[string]string{}
 for path,raw:=range files{target:=filepath.Join(npm,path);if os.MkdirAll(filepath.Dir(target),0700)!=nil||os.WriteFile(target,raw,0600)!=nil{t.Fatal("npm fixture")};hashes[path]=digest(raw)}
 nodeBytes:=[]byte("synthetic node never executed");if os.MkdirAll(filepath.Dir(node),0700)!=nil||os.WriteFile(node,nodeBytes,0700)!=nil{t.Fatal("node fixture")}
 raw,e:=json.Marshal(hashes);if e!=nil{t.Fatal(e)}
 if digest(raw)!="d1ed09651791dff42c49a42d36d08c75bd13939b9728949451d1a61f036110a2"{t.Fatal("shared synthetic canonical digest mismatch")}
 got,e:=snapshot(npm);if e!=nil||mapDigest(got)!=digest(raw){t.Fatal("tree contract mismatch")}
 fixture:=Tool{Node:node,NodeSHA256:digest(nodeBytes),NpmRoot:npm,NpmTreeSHA256:digest(raw)}
 if tool(fixture,source)!=nil{t.Fatal("matching complete synthetic tree refused")}
 if os.WriteFile(filepath.Join(npm,"bin","npm-cli.js"),[]byte("changed"),0600)!=nil{t.Fatal("drift fixture")}
 if tool(fixture,source)==nil{t.Fatal("complete npm tree drift accepted")}
 if os.WriteFile(filepath.Join(npm,"bin","npm-cli.js"),files["bin/npm-cli.js"],0600)!=nil{t.Fatal("restore")}
 if os.WriteFile(node,[]byte("changed"),0700)!=nil{t.Fatal("node drift")}
 if tool(fixture,source)==nil{t.Fatal("node byte drift accepted")}
}
