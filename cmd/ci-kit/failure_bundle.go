package main

import (
	"filippo.io/age"
	"os"
	"path/filepath"
	"regexp"

	"github.com/keeldock-platform/ci-kit/internal/config"
	"github.com/keeldock-platform/ci-kit/internal/diagnostics"
	"github.com/keeldock-platform/ci-kit/internal/process"
)

func diagnosticRecipient() (string, error) {
	wd, e := os.Getwd()
	if e != nil {
		return "", refused
	}
	raw, e := readRegular(filepath.Join(wd, "public-control", ".github", "diagnostics.json"), 4096)
	if e != nil {
		return "", refused
	}
	var cfg struct {
		SchemaVersion int    `json:"schema_version"`
		Recipient     string `json:"recipient"`
	}
	if config.Decode(raw, &cfg, 4096) != nil || cfg.SchemaVersion != 1 {
		return "", refused
	}
	if _, err := age.ParseX25519Recipient(cfg.Recipient); err != nil {
		return "", refused
	}
	return cfg.Recipient, nil
}

func sealFailure(sink, recipient string, c control, code int) error {
	run, attempt := os.Getenv("CI_RUN_ID"), os.Getenv("CI_ATTEMPT")
	numeric := regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	if !numeric.MatchString(run) || !numeric.MatchString(attempt) {
		return refused
	}
	slot := os.Getenv("CI_PLATFORM")
	if os.Getenv("CI_KIT_OPERATION") == "plan" {
		slot = "plan"
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,40}$`).MatchString(slot) {
		return refused
	}
	out := filepath.Join(os.Getenv("RUNNER_TEMP"), "ci-diagnostics")
	if !process.UserOnly(out, true) && process.MakePrivateDir(out) != nil {
		return refused
	}
	identity := diagnostics.Identity{Run: run, Attempt: attempt, Platform: slot, KitSHA: c.Policy.KitSHA, SourceSHA: c.Request.Inputs["source_sha"]}
	return diagnostics.Seal(sink, filepath.Join(out, "failure-"+slot+"-"+attempt+".age"), recipient, identity, code)
}
