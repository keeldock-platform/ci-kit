package main

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "io"
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
const (
 lockFailPrivateSetup=10
 lockFailCleanup=11
 lockFailControlHead=12
 lockFailClone=13
 lockFailMainHead=14
 lockFailFetch=15
 lockFailCheckout=16
 lockFailInputHead=17
 lockFailInputTree=18
 lockFailInputLock=19
 lockFailBeforeSnapshot=20
 lockFailGenerate=21
 lockFailAfterSnapshot=22
 lockFailAfterChange=23
 lockFailVerify=24
 lockFailFinalSnapshot=25
 lockFailPostVerifyChange=26
 lockFailFinalRead=27
 lockFailOutput=28
)
type lockOperation struct {
 Schema int `json:"schema_version"`
 InputCommit string `json:"input_commit"`
 InputTree string `json:"input_tree"`
 Base string `json:"base"`
 InputLockSHA256 string `json:"input_lock_sha256"`
}

const maxVerifyFindings = 4096
const maxVerifyCount = 4096

type verifyFinding struct {
 Category string `json:"category"`
 Severity string `json:"severity"`
 Retryable bool `json:"retryable,omitempty"`
}

type verifyCounts struct {
 values map[string]map[string]int
 unknown int
}

type verifyDiagnostic struct {
 result string
 parseRejection string
 conditions map[string]bool
 counts verifyCounts
}

var verifyResultCategories = []string{
 "request_rejected", "tool_exit_1_findings", "tool_exit_2", "other_tool_exit",
 "capture_or_start_failure", "capture_read_failure", "json_invalid", "unknown_category",
}
var verifyResultConditions = []string{"timed_out", "interrupted", "capture_overflow", "cleanup_incomplete", "signal_terminated"}
var verifyParseRejections = []string{"empty_or_oversized", "malformed_json", "envelope_shape", "version_field", "findings_field", "findings_shape", "finding_limit", "count_limit"}

var verifyCategories = []string{
 "not-pinned", "ref-missing", "invalid-workflow", "sha-as-ref", "ref-changed", "ref-moved",
 "tag-commit-mismatch", "stale", "misleading-sha", "unreachable-pin", "repo-renamed",
 "repo-replaced", "repo-unavailable", "valid", "run-only", "ancestry-unknown",
 "reachability-unknown", "onboarding-required", "version-ref", "local-action", "stale-workflow",
 "self-repository-action", "invalid-self-repository-ref", "fresh-tag", "cooldown-config-ignored",
 "incomplete-lockfile", "container-image",
}

var verifySeverities = []string{"error", "warning", "info", "ok"}

func parseVerifyFindings(raw []byte) (verifyCounts, bool) {
 counts, ok, _ := parseVerifyFindingsReason(raw)
 return counts, ok
}

func parseVerifyFindingsReason(raw []byte) (verifyCounts, bool, string) {
 counts := verifyCounts{values: make(map[string]map[string]int)}
 if len(raw) == 0 || len(raw) > 2<<20 {
  return counts, false, "empty_or_oversized"
 }
 var envelope map[string]json.RawMessage
 if json.Unmarshal(raw, &envelope) != nil {
  return counts, false, "malformed_json"
 }
 if len(envelope) != 3 {
  return counts, false, "envelope_shape"
 }
 for _, key := range []string{"cli_version", "lockfile_version"} {
  var value string
  if json.Unmarshal(envelope[key], &value) != nil || value == "" {
   return counts, false, "version_field"
  }
 }
 rawFindings, ok := envelope["findings"]
 rawFindings = bytes.TrimSpace(rawFindings)
 if !ok || len(rawFindings) == 0 || rawFindings[0] != '[' {
  return counts, false, "findings_field"
 }
 var findings []verifyFinding
 if json.Unmarshal(rawFindings, &findings) != nil {
  return counts, false, "findings_shape"
 }
 if len(findings) > maxVerifyFindings {
  return counts, false, "finding_limit"
 }
 for _, finding := range findings {
  if !knownVerifyValue(verifyCategories, finding.Category) || !knownVerifyValue(verifySeverities, finding.Severity) {
   counts.unknown++
   continue
  }
  if counts.values[finding.Category] == nil {
   counts.values[finding.Category] = make(map[string]int)
  }
  current := counts.values[finding.Category][finding.Severity]
  if current >= maxVerifyCount {
   return verifyCounts{values: make(map[string]map[string]int)}, false, "count_limit"
  }
  counts.values[finding.Category][finding.Severity] = current + 1
 }
 return counts, true, ""
}

func knownVerifyValue(allowed []string, value string) bool {
 for _, candidate := range allowed {
  if candidate == value {
   return true
  }
 }
 return false
}

func verifyFailureDiagnostic(r process.Result, err error) verifyDiagnostic {
 d := verifyDiagnostic{
  conditions: make(map[string]bool),
  counts: verifyCounts{values: make(map[string]map[string]int)},
 }
 switch {
 case errors.Is(err, process.ErrInvalid):
  d.result = "request_rejected"
 case r.ExitCode == 1 && r.ExitObserved:
  d.result = "tool_exit_1_findings"
 case r.ExitCode == 2:
  d.result = "tool_exit_2"
 case r.ExitCode > 0:
  d.result = "other_tool_exit"
 default:
  d.result = "capture_or_start_failure"
 }
 if !errors.Is(err, process.ErrInvalid) {
  if r.TimedOut { d.conditions["timed_out"] = true }
  if r.Interrupted { d.conditions["interrupted"] = true }
  if r.Overflow { d.conditions["capture_overflow"] = true }
  if !r.CleanupComplete { d.conditions["cleanup_incomplete"] = true }
  if r.Signal != "" { d.conditions["signal_terminated"] = true }
 }
 return d
}

func verifyFindingsEligible(d verifyDiagnostic) bool {
 return d.result == "tool_exit_1_findings" && len(d.conditions) == 0
}

func emitVerifyDiagnostic(d verifyDiagnostic) {
 _ = writeVerifyDiagnostic(os.Stdout, d)
}

func writeVerifyDiagnostic(w io.Writer, d verifyDiagnostic) error {
 if !knownVerifyValue(verifyResultCategories, d.result) {
  return errors.New("invalid fixed verifier result")
 }
 for condition, enabled := range d.conditions {
  if enabled && !knownVerifyValue(verifyResultConditions, condition) {
   return errors.New("invalid fixed verifier condition")
  }
 }
 if d.parseRejection != "" && !knownVerifyValue(verifyParseRejections, d.parseRejection) {
  return errors.New("invalid fixed verifier parse rejection")
 }
 for category, bySeverity := range d.counts.values {
  if !knownVerifyValue(verifyCategories, category) {
   return errors.New("invalid fixed verifier category")
  }
  for severity, count := range bySeverity {
   if !knownVerifyValue(verifySeverities, severity) || count < 0 || count > maxVerifyCount {
    return errors.New("invalid fixed verifier count")
   }
  }
 }
 if _, err := fmt.Fprintf(w, "VERIFY_DIAGNOSTIC result=%s\n", d.result); err != nil {
  return err
 }
 if d.parseRejection != "" {
  if _, err := fmt.Fprintf(w, "VERIFY_DIAGNOSTIC parse_rejection=%s\n", d.parseRejection); err != nil { return err }
 }
 for _, condition := range verifyResultConditions {
  if d.conditions[condition] {
   if _, err := fmt.Fprintf(w, "VERIFY_DIAGNOSTIC condition=%s\n", condition); err != nil {
    return err
   }
  }
 }
 for _, category := range verifyCategories {
  for _, severity := range verifySeverities {
   count := d.counts.values[category][severity]
   if count < 0 || count > maxVerifyCount {
    return errors.New("invalid fixed verifier count")
   }
   if count > 0 {
    if _, err := fmt.Fprintf(w, "VERIFY_FINDING category=%s severity=%s count=%d\n", category, severity, count); err != nil {
     return err
    }
   }
  }
 }
 if d.counts.unknown < 0 || d.counts.unknown > maxVerifyFindings {
  return errors.New("invalid fixed verifier count")
 }
 if d.counts.unknown > 0 {
  if _, err := fmt.Fprintf(w, "VERIFY_FINDING category=unknown severity=unknown count=%d\n", d.counts.unknown); err != nil {
   return err
  }
 }
 return nil
}
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
 control,e:=os.Getwd();if e!=nil{return lockFailPrivateSetup}
 var op lockOperation
 if config.Read(filepath.Join(control,"docs","action-lock-operation.json"),&op,4096)!=nil||op.Schema!=1||!protocol.SHA(op.InputCommit)||op.InputTree!=lockInputTree||op.Base!="b59374a9976d240b03387eaac7d91ca4a5697ef5"||!protocol.Digest(op.InputLockSHA256){return 3}
 parent:=os.Getenv("RUNNER_TEMP");if _,e:=process.SafePath(parent,".");e!=nil{return 3}
 private,e:=os.MkdirTemp(parent,"action-lock-private-");if e!=nil{return lockFailPrivateSetup}
 defer func(){if os.RemoveAll(private)!=nil&&code==0{code=lockFailCleanup}}()
 target:=filepath.Join(private,"target");index:=0
 call:=func(root string,argv []string,auth bool)([]byte,error){
  index++;spec:=process.Spec{Argv:argv,Root:root,Cwd:".",ResultDir:filepath.Join(private,"capture-"+strconv.Itoa(index)),Timeout:2*time.Minute,OutputLimit:2<<20,Env:map[string]string{"HOME":private,"PATH":"/usr/bin:/bin","CI":"true"}}
  var r process.Result;var err error
  if auth{r,err=process.RunPublicActionLock(ctx,spec,os.Getenv("GH_TOKEN"))}else{r,err=process.Run(ctx,spec)}
  if err!=nil||r.ExitCode!=0||!r.CleanupComplete||r.TimedOut||r.Overflow{return nil,refused}
  return readRegular(r.StdoutPath,2<<20)
 }
 verify := func() verifyDiagnostic {
  index++
  spec := process.Spec{
   Argv: []string{filepath.Join(parent, "policy-tools", "gh-actions-lock"), "--verify", "--fail-on-warning", "--json=findings"},
   Root: target,
   Cwd: ".",
   ResultDir: filepath.Join(private, "capture-"+strconv.Itoa(index)),
   Timeout: 2 * time.Minute,
   OutputLimit: 2 << 20,
   Env: map[string]string{"HOME": private, "PATH": "/usr/bin:/bin", "CI": "true"},
  }
  r, err := process.RunPublicActionLock(ctx, spec, os.Getenv("GH_TOKEN"))
  d := verifyFailureDiagnostic(r, err)
  if err == nil && r.ExitCode == 0 && r.CleanupComplete && !r.TimedOut && !r.Overflow {
   return verifyDiagnostic{}
  }
  if verifyFindingsEligible(d) {
   raw, readErr := readRegular(r.StdoutPath, 2<<20)
   if readErr != nil {
    d.result = "capture_read_failure"
    return d
   }
   counts, valid, reason := parseVerifyFindingsReason(raw)
   if !valid {
    d.result = "json_invalid"
    d.parseRejection = reason
    return d
   }
   d.counts = counts
   if counts.unknown > 0 {
    d.result = "unknown_category"
   }
  }
  return d
 }
 controlHead,e:=call(control,[]string{"/usr/bin/git","rev-parse","HEAD"},false);if e!=nil{return lockFailControlHead};if !protocol.SHA(os.Getenv("GITHUB_SHA"))||strings.TrimSpace(string(controlHead))!=os.Getenv("GITHUB_SHA"){return 3}
 if _,e:=call(control,[]string{"/usr/bin/git","clone","--no-checkout","--depth=1","https://github.com/keeldock-platform/ci-kit.git",target},false);e!=nil{return lockFailClone}
 current,e:=call(target,[]string{"/usr/bin/git","rev-parse","origin/main"},false);if e!=nil{return lockFailMainHead};if strings.TrimSpace(string(current))!=op.Base{return 3}
 if _,e:=call(target,[]string{"/usr/bin/git","fetch","--depth=1","origin",op.InputCommit},false);e!=nil{return lockFailFetch}
 if _,e:=call(target,[]string{"/usr/bin/git","-c","core.hooksPath=/dev/null","checkout","--detach",op.InputCommit},false);e!=nil{return lockFailCheckout}
 head,e:=call(target,[]string{"/usr/bin/git","rev-parse","HEAD"},false);if e!=nil{return lockFailInputHead};if strings.TrimSpace(string(head))!=op.InputCommit{return 3}
 tree,e:=call(target,[]string{"/usr/bin/git","rev-parse","HEAD^{tree}"},false);if e!=nil{return lockFailInputTree};if strings.TrimSpace(string(tree))!=op.InputTree{return 3}
 old,e:=readRegular(filepath.Join(target,generatedActionLock),2<<20);if e!=nil||actionDigest(old)!=op.InputLockSHA256{return lockFailInputLock}
 before,e:=actionSnapshot(target);if e!=nil{return lockFailBeforeSnapshot}
 tool:=filepath.Join(parent,"policy-tools","gh-actions-lock")
 if _,e=call(target,[]string{tool,"--no-narrow"},true);e!=nil{return lockFailGenerate}
 after,e:=actionSnapshot(target);if e!=nil{return lockFailAfterSnapshot};if !onlyActionLock(before,after){return lockFailAfterChange}
 verifyDiag:=verify()
 if verifyDiag.result!="" { emitVerifyDiagnostic(verifyDiag); return lockFailVerify }
 end,e:=actionSnapshot(target);if e!=nil{return lockFailFinalSnapshot};if !reflect.DeepEqual(after,end){return lockFailPostVerifyChange}
 raw,e:=readRegular(filepath.Join(target,generatedActionLock),2<<20);if e!=nil||len(raw)==0{return lockFailFinalRead}
 // Only the genuine generated lock is returned. No source archive/diff/log,
 // command, environment, receipt assertion or automatic commit is published.
 output:=filepath.Join(parent,"generated-action-lock");if process.MakePrivateDir(output)!=nil{return lockFailOutput}
 f,e:=process.CreatePrivateFile(filepath.Join(output,"actions.lock"));if e!=nil{return lockFailOutput}
 n,e:=f.Write(raw);ce:=f.Close();if e!=nil||ce!=nil||n!=len(raw){return lockFailOutput}
 return 0
}
// The existing main prints only fixed PASS/FAIL. This operation is generation,
// never whole-kit policy, person approval, release or admission evidence.
