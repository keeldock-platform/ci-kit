package protocol

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/keeldock-platform/ci-kit/schemas"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Dispatch struct {
	Ref    string            `json:"ref"`
	Inputs map[string]string `json:"inputs"`
}

func NewUUID() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	s := hex.EncodeToString(b)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}
func NewDispatch(concern, source, base, mode, purpose, platforms, selector, id string) (Dispatch, error) {
	d := Dispatch{"main", map[string]string{"concern": concern, "source_sha": source, "base_sha": base, "mode": mode, "purpose": purpose, "platforms": platforms, "selector": selector, "request_id": id, "kit_api": "1"}}
	b, _ := json.Marshal(d.Inputs)
	h := sha256.Sum256(b)
	d.Inputs["request_digest"] = hex.EncodeToString(h[:])
	if e := d.Validate(); e != nil {
		return d, e
	}
	return d, nil
}

var concernPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,95}$`)

func (d Dispatch) Validate() error {
	bad := errors.New("invalid dispatch")
	if len(d.RunName()) > 160 || d.Ref != "main" || len(d.Inputs) != 10 {
		return bad
	}
	for _, k := range []string{"concern", "source_sha", "base_sha", "mode", "purpose", "platforms", "selector", "request_id", "kit_api", "request_digest"} {
		if d.Inputs[k] == "" {
			return bad
		}
	}
	i := d.Inputs
	if !concernPattern.MatchString(i["concern"]) || !SHA(i["source_sha"]) || !SHA(i["base_sha"]) || !UUID(i["request_id"]) || i["kit_api"] != "1" || !slices.Contains([]string{"quick", "unit", "affected", "full", "candidate"}, i["mode"]) || !slices.Contains([]string{"feedback", "admission"}, i["purpose"]) {
		return bad
	}
	if i["purpose"] == "admission" && (i["platforms"] != "all" || i["selector"] != "all" || i["mode"] == "quick" || i["mode"] == "unit") {
		return bad
	}
	if i["platforms"] != "all" {
		ps := strings.Split(i["platforms"], ",")
		last := ""
		for _, p := range ps {
			if !slices.Contains(Platforms, p) || p <= last {
				return bad
			}
			last = p
		}
	}
	if i["selector"] != "all" {
		ss := strings.Split(i["selector"], ":")
		if len(ss) != 3 || ss[0] != "v1" || !Digest(ss[1]) {
			return bad
		}
		ns := strings.Split(ss[2], ",")
		if len(ns) > 256 {
			return bad
		}
		last := -1
		for _, n := range ns {
			x, e := strconv.Atoi(n)
			if e != nil || x < 0 || x > 10000000 || x <= last || strconv.Itoa(x) != n {
				return bad
			}
			last = x
		}
	}
	m := map[string]string{}
	for k, v := range i {
		if k != "request_digest" {
			m[k] = v
		}
	}
	b, _ := json.Marshal(m)
	h := sha256.Sum256(b)
	if i["request_digest"] != hex.EncodeToString(h[:]) {
		return bad
	}
	b, _ = json.Marshal(d)
	var raw any
	if json.Unmarshal(b, &raw) != nil || schemas.Validate("urn:keeldock:kd-ci:dispatch:1", raw) != nil {
		return bad
	}
	if len(b) > 8192 {
		return bad
	}
	return nil
}
func (d Dispatch) RunName() string {
	return fmt.Sprintf("kd/%s/%s/%s/%s", d.Inputs["concern"], d.Inputs["purpose"], d.Inputs["request_id"], d.Inputs["request_digest"])
}
