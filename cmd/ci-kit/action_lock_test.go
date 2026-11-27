package main

// Source-free file-output/drift guards. AUTHORED / NOT RUN. These synthetic map
// values are not actions.lock contents, a generated lock or a tool receipt.
import "testing"
func TestActionLockGeneratedFileOnly(t *testing.T){
 before:=map[string]string{generatedActionLock:"old:600",".github/workflows/ci-policy.yml":"reviewed:644"}
 for _,c:=range []struct{name string;after map[string]string;want bool}{
  {"one-genuine-changed-output",map[string]string{generatedActionLock:"new:600",".github/workflows/ci-policy.yml":"reviewed:644"},true},
  {"unchanged",map[string]string{generatedActionLock:"old:600",".github/workflows/ci-policy.yml":"reviewed:644"},false},
  {"workflow-rewrite",map[string]string{generatedActionLock:"new:600",".github/workflows/ci-policy.yml":"rewritten:644"},false},
  {"added-file",map[string]string{generatedActionLock:"new:600",".github/workflows/ci-policy.yml":"reviewed:644","extra":"new"},false},
  {"removed-file",map[string]string{generatedActionLock:"new:600"},false},
 }{t.Run(c.name,func(t *testing.T){if onlyActionLock(before,c.after)!=c.want{t.Fatal("generated-only refusal mismatch")}})}
}
