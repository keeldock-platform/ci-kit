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
	"regexp"
 "reflect"
 "strconv"
 "strings"
 "time"

 "github.com/keeldock-platform/ci-kit/internal/config"
 "github.com/keeldock-platform/ci-kit/internal/process"
 "github.com/keeldock-platform/ci-kit/internal/protocol"
)
const generatedActionLock=".github/workflows/actions.lock"
const lockInputCommit="ff0d992788d5fedf81b178b73bff45172aad9840"
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

type verifyCounts struct {
 values map[string]map[string]int
 unknown int
	unknownInfo int
	nativeValid bool
	policyCompatible bool
	shaAsRefWarnings int
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
var verifyResultConditions = []string{"timed_out", "interrupted", "capture_overflow", "capture_incomplete", "capture_read_failure", "cleanup_incomplete", "signal_terminated"}
var verifyParseRejections = []string{"empty_or_oversized", "malformed_json", "envelope_shape", "policy_key_alias", "version_field", "valid_field", "findings_field", "findings_shape", "finding_limit", "count_limit"}
var verifySHARefDependency = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[a-f0-9]{40}$`)

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
	counts := verifyCounts{values: make(map[string]map[string]int), policyCompatible: true}
 if len(raw) == 0 || len(raw) > 2<<20 {
  return counts, false, "empty_or_oversized"
 }
	if config.ValidateJSON(raw, 2<<20) != nil {
		return counts, false, "malformed_json"
	}
 var envelope map[string]json.RawMessage
 if json.Unmarshal(raw, &envelope) != nil {
  return counts, false, "malformed_json"
 }
	if hasVerifyKeyAlias(envelope, "cli_version", "lockfile_version", "valid", "findings") {
		return counts, false, "policy_key_alias"
	}
	if len(envelope) != 4 {
		return counts, false, "envelope_shape"
	}
 for _, key := range []string{"cli_version", "lockfile_version"} {
  var value string
  if json.Unmarshal(envelope[key], &value) != nil || value == "" || bytes.Equal(bytes.TrimSpace(envelope[key]), []byte("null")) {
   return counts, false, "version_field"
  }
 }
	validRaw, exists := envelope["valid"]
	if !exists || (string(bytes.TrimSpace(validRaw)) != "true" && string(bytes.TrimSpace(validRaw)) != "false") || json.Unmarshal(validRaw, &counts.nativeValid) != nil {
		return counts, false, "valid_field"
	}
 rawFindings, ok := envelope["findings"]
 rawFindings = bytes.TrimSpace(rawFindings)
 if !ok || len(rawFindings) == 0 || rawFindings[0] != '[' {
  return counts, false, "findings_field"
 }
	var findings []json.RawMessage
	if json.Unmarshal(rawFindings, &findings) != nil || findings == nil {
  return counts, false, "findings_shape"
 }
 if len(findings) > maxVerifyFindings {
  return counts, false, "finding_limit"
 }
	for _, rawFinding := range findings {
		var finding map[string]json.RawMessage
		if len(bytes.TrimSpace(rawFinding)) == 0 || json.Unmarshal(rawFinding, &finding) != nil || finding == nil {
			return counts, false, "findings_shape"
		}
		if hasVerifyKeyAlias(finding, "severity") { return counts, false, "policy_key_alias" }
		severity, hasSeverity := exactVerifyString(finding, "severity")
		if !hasSeverity || severity == "" { return counts, false, "findings_shape" }
		if severity == "info" {
			category, hasCategory := exactVerifyString(finding, "category")
			if hasCategory && knownVerifyValue(verifyCategories, category) {
				if !addVerifyCount(&counts, category, severity) { return verifyCounts{values: make(map[string]map[string]int)}, false, "count_limit" }
			} else {
				counts.unknownInfo++
			}
			continue
		}
		if hasVerifyKeyAlias(finding, "category", "dependency") { return counts, false, "policy_key_alias" }
		category, hasCategory := exactVerifyString(finding, "category")
		if _, exists := finding["category"]; exists && !hasCategory { return counts, false, "findings_shape" }
		dependency, hasDependency := exactVerifyString(finding, "dependency")
		if _, exists := finding["dependency"]; exists && !hasDependency { return counts, false, "findings_shape" }
		if severity == "warning" && hasCategory && hasDependency && category == "sha-as-ref" && verifySHARefDependency.MatchString(dependency) {
			if !addVerifyCount(&counts, category, severity) { return verifyCounts{values: make(map[string]map[string]int)}, false, "count_limit" }
			counts.shaAsRefWarnings++
			continue
		}
		counts.policyCompatible = false
		if !hasCategory || !knownVerifyValue(verifyCategories, category) || !knownVerifyValue(verifySeverities, severity) {
			counts.unknown++
			continue
		}
		if !addVerifyCount(&counts, category, severity) { return verifyCounts{values: make(map[string]map[string]int)}, false, "count_limit" }
 }
 return counts, true, ""
}

func hasVerifyKeyAlias(fields map[string]json.RawMessage, canonical ...string) bool {
	for key := range fields {
		for _, name := range canonical {
			if key != name && strings.EqualFold(key, name) { return true }
		}
	}
	return false
}

func exactVerifyString(fields map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := fields[key]
	if !ok || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) { return "", false }
	var value string
	if json.Unmarshal(raw, &value) != nil { return "", false }
	return value, true
}

func addVerifyCount(counts *verifyCounts, category, severity string) bool {
	if counts.values[category] == nil { counts.values[category] = make(map[string]int) }
	if counts.values[category][severity] >= maxVerifyCount { return false }
	counts.values[category][severity]++
	return true
}

func kWorkflowLockCompatibility(counts verifyCounts, immutableRefsPassed bool) bool {
	// Mirrors K's accepted() exception only after its separate immutable-ref gate.
	return counts.nativeValid && counts.policyCompatible && counts.unknown == 0 && counts.shaAsRefWarnings > 0 && immutableRefsPassed
}

func immutableActionPolicyPassed(output []byte) bool {
	return bytes.Equal(output, []byte("PASS immutable_actions\n"))
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
  if !r.CaptureComplete { d.conditions["capture_incomplete"] = true }
  if !r.CleanupComplete { d.conditions["cleanup_incomplete"] = true }
  if r.Signal != "" { d.conditions["signal_terminated"] = true }
  if err != nil && !errors.Is(err, process.ErrCapture) && !errors.Is(err, process.ErrOverflow) && !errors.Is(err, process.ErrCleanup) {
   d.conditions["capture_read_failure"] = true
  }
 }
 return d
}

func verifyFindingsEligible(d verifyDiagnostic) bool {
 return d.result == "tool_exit_1_findings" && len(d.conditions) == 0
}

func parseVerifyFindingsOutput(d verifyDiagnostic, raw []byte, readErr error) verifyDiagnostic {
	if !verifyFindingsEligible(d) { return d }
	if readErr != nil {
		d.result = "capture_read_failure"
		d.conditions["capture_read_failure"] = true
		return d
	}
	counts, valid, reason := parseVerifyFindingsReason(raw)
	if !valid {
		d.result = "json_invalid"
		d.parseRejection = reason
		return d
	}
	d.counts = counts
	if counts.unknown > 0 { d.result = "unknown_category" }
	return d
}

func admitKWorkflowLockCompatibility(d verifyDiagnostic, immutableResult []byte, immutableErr error) bool {
	return verifyFindingsEligible(d) && kWorkflowLockCompatibility(d.counts, immutableErr == nil && immutableActionPolicyPassed(immutableResult))
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
 if d.counts.unknownInfo < 0 || d.counts.unknownInfo > maxVerifyFindings {
  return errors.New("invalid fixed verifier count")
 }
 if d.counts.unknown > 0 {
  if _, err := fmt.Fprintf(w, "VERIFY_FINDING category=unknown severity=unknown count=%d\n", d.counts.unknown); err != nil {
   return err
  }
 }
 if d.counts.unknownInfo > 0 {
  if _, err := fmt.Fprintf(w, "VERIFY_FINDING category=unknown severity=info count=%d\n", d.counts.unknownInfo); err != nil { return err }
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
 if config.Read(filepath.Join(control,"docs","action-lock-operation.json"),&op,4096)!=nil||op.Schema!=1||op.InputCommit!=lockInputCommit||op.InputTree!=lockInputTree||op.Base!="b59374a9976d240b03387eaac7d91ca4a5697ef5"||!protocol.Digest(op.InputLockSHA256){return 3}
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
   Argv: []string{filepath.Join(parent, "policy-tools", "gh-actions-lock"), "--verify", "--fail-on-warning", "--json=valid,findings"},
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
		d = parseVerifyFindingsOutput(d, raw, readErr)
		if d.result == "json_invalid" || d.result == "capture_read_failure" { return d }
		if d.result != "unknown_category" && d.counts.nativeValid && d.counts.policyCompatible && d.counts.unknown == 0 && d.counts.shaAsRefWarnings > 0 {
    // This fixed no-token check runs only the unchanged script from the exact S checkout above.
    immutableResult, immutableErr := call(target, []string{"/usr/bin/python3", "-I", "-B", filepath.Join(target, "tests/policy/immutable_actions.py"), "--root", target}, false)
			if admitKWorkflowLockCompatibility(d, immutableResult, immutableErr) {
     return verifyDiagnostic{}
    }
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
