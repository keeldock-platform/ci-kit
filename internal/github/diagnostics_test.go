package github

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticDownloadUsesRealGoghRedirectWithoutAuth(t *testing.T) {
	x := fixture()
	slot := x.Registration.Platforms[0]
	for _, tc := range []struct {
		name, artifactSlot, location string
		failed                       []string
		want                         bool
	}{
		{"valid", slot, "https://results.blob.core.windows.net/file?signed=fixture", []string{slot}, true},
		{"missing-slot", slot, "https://results.blob.core.windows.net/file", []string{slot, "plan"}, false},
		{"passed-slot", slot, "https://results.blob.core.windows.net/file", []string{"plan"}, false},
		{"hostile-redirect", slot, "https://example.com/file", []string{slot}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var z bytes.Buffer
			w := zip.NewWriter(&z)
			f, _ := w.Create("failure-" + tc.artifactSlot + "-1.age")
			f.Write([]byte("synthetic ciphertext"))
			w.Close()
			apiReads, downloads := 0, 0
			c, e := New("synthetic-credential", time.Second, transportFunc(func(r *http.Request) (*http.Response, error) {
				apiReads++
				if !strings.Contains(r.Header.Get("Authorization"), "synthetic-credential") {
					t.Fatal("API auth absent")
				}
				if strings.HasSuffix(r.URL.Path, "/jobs") {
					rows := []any{}
					for _, s := range tc.failed {
						name := "pack-" + s
						if s == "plan" {
							name = "plan"
						}
						rows = append(rows, map[string]any{"run_id": 4, "run_attempt": 1, "name": name, "steps": []any{map[string]any{"name": "Execute", "conclusion": "failure"}}})
					}
					return reply(200, map[string]any{"total_count": len(rows), "jobs": rows}), nil
				}
				if strings.Contains(r.URL.Path, "/artifacts/") {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{tc.location}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				return reply(200, map[string]any{"total_count": 1, "artifacts": []any{map[string]any{"id": 99, "name": "diagnostics-" + tc.artifactSlot + "-1", "size_in_bytes": z.Len(), "expired": false, "workflow_run": map[string]any{"id": 4, "head_sha": x.Registration.ControlSHA}}}}), nil
			}))
			if e != nil {
				t.Fatal(e)
			}
			c.ArtifactTransport = transportFunc(func(r *http.Request) (*http.Response, error) {
				downloads++
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatal("auth forwarded to storage")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(z.Bytes())), Request: r}, nil
			})
			files, e := c.DiagnosticArtifacts(context.Background(), x, tc.failed)
			if tc.want && (e != nil || string(files[slot]) != "synthetic ciphertext" || downloads != 1 || apiReads != 3) {
				t.Fatalf("valid redirect failed %v requests=%d/%d", e, apiReads, downloads)
			}
			if !tc.want && e == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}

func TestDiagnosticZipBoundBeforeAllocation(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	f, _ := z.Create("failure-test-1.age")
	f.Write([]byte("cipher"))
	z.Close()
	raw := b.Bytes()
	if !boundedDiagnosticZip(raw) {
		t.Fatal("provider-shaped zip refused")
	}
	for _, offset := range []int{4, 6, 8, 10, 12, 16, 20} {
		bad := append([]byte{}, raw...)
		bad[len(bad)-22+offset] = 255
		if boundedDiagnosticZip(bad) {
			t.Fatal("oversized or ZIP64 footer accepted")
		}
	}
	var many bytes.Buffer
	z = zip.NewWriter(&many)
	z.Create("one")
	z.Create("two")
	z.Close()
	if boundedDiagnosticZip(many.Bytes()) {
		t.Fatal("multi-entry archive accepted")
	}
}
func FuzzDiagnosticZipFooter(f *testing.F) {
	f.Add([]byte("PK"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 1<<20 {
			return
		}
		_ = boundedDiagnosticZip(raw)
	})
}
