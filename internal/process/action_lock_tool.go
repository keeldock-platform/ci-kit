package process

// This new bridge is only for the checksum-pinned source-free public lock tool.
// It grants no project/source-read credential escape hatch. Requires separate
// exact security review; current production Run/RunTool behavior is unchanged.
import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "os"
 "strings"
)

func validPublicActionLockArgv(argv []string) bool {
 if len(argv)==2&&argv[1]=="--no-narrow" { return true }
 if len(argv)==3&&argv[1]=="--verify"&&argv[2]=="--fail-on-warning" { return true }
 return len(argv)==4&&argv[1]=="--verify"&&argv[2]=="--fail-on-warning"&&argv[3]=="--json=valid,findings"
}

func RunPublicActionLock(ctx context.Context,s Spec,token string)(Result,error){
 if token==""||strings.ContainsAny(token,"\x00\r\n")||len(s.Argv)<2||len(s.Argv)>5||s.Cwd!="."||!regularToolPath(s.Argv[0])||!outsideSource(s.Root,s.Argv[0]){return Result{ExitCode:-1},ErrInvalid}
 if !validPublicActionLockArgv(s.Argv){return Result{ExitCode:-1},ErrInvalid}
 st,e:=os.Stat(s.Argv[0]);if e!=nil||st.Size()>256<<20{return Result{ExitCode:-1},ErrInvalid}
 raw,e:=os.ReadFile(s.Argv[0]);if e!=nil{return Result{ExitCode:-1},ErrInvalid}
 hash:=sha256.Sum256(raw)
 if hex.EncodeToString(hash[:])!="94009f287cfcc046ff371824824c203a8a44c981ca2dac344052744897c29c3a"{return Result{ExitCode:-1},ErrInvalid}
 return run(ctx,s,map[string]string{"GH_TOKEN":token})
}
