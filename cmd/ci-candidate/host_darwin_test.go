package main

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDarwinNativeHostQueries(t *testing.T) {
	cases := []struct {
		name       string
		translated uint32
		probeErr   error
		machine    string
		machineErr error
		want       string
		reason     string
		machineRun bool
	}{
		{"Intel missing translation key", 0, unix.ENOENT, "x86_64", nil, "amd64", "", true},
		{"Intel native", 0, nil, "x86_64\n", nil, "amd64", "", true},
		{"ARM native", 0, nil, "arm64", nil, "arm64", "", true},
		{"ARM missing translation key", 0, unix.ENOENT, "arm64", nil, "arm64", "", true},
		{"translated Intel process", 1, nil, "x86_64", nil, "", "translation", false},
		{"invalid translation value", 2, nil, "x86_64", nil, "", "translation-value", false},
		{"permission error is not Intel", 0, unix.EPERM, "x86_64", nil, "", "translation-query", false},
		{"query fault is not Intel", 0, unix.EIO, "x86_64", nil, "", "translation-query", false},
		{"machine query fails", 0, unix.ENOENT, "x86_64", unix.EIO, "", "native-query", true},
		{"unknown machine", 0, nil, "i386", nil, "", "native-value", true},
		{"malformed machine", 0, nil, "x86_64\narm64", nil, "", "native-value", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machineRun := false
			got, err := darwinNativeArchitecture(context.Background(), func(key string) (uint32, error) {
				if key != "sysctl.proc_translated" {
					t.Fatalf("unexpected integer query %q", key)
				}
				return tc.translated, tc.probeErr
			}, func(key string) (string, error) {
				if key != "hw.machine" {
					t.Fatalf("unexpected text query %q", key)
				}
				machineRun = true
				return tc.machine, tc.machineErr
			})
			if got != tc.want || machineRun != tc.machineRun {
				t.Fatalf("architecture %q, machineRun %v; want %q, %v", got, machineRun, tc.want, tc.machineRun)
			}
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, hostCheckError(tc.reason)) || candidateFailureLine(err) != "FAIL: candidate host "+tc.reason {
				t.Fatalf("wrong propagated reason: %v", err)
			}
		})
	}
}

func TestDarwinHostCancellationRefusesQueries(t *testing.T) {
	for _, cancelAtProbe := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if !cancelAtProbe {
			cancel()
		}
		_, err := darwinNativeArchitecture(ctx, func(key string) (uint32, error) {
			if !cancelAtProbe {
				t.Fatal("query after cancellation")
			}
			cancel()
			return 0, nil
		}, func(string) (string, error) {
			t.Fatal("machine query after cancellation")
			return "x86_64", nil
		})
		cancel()
		if !errors.Is(err, hostCheckError("native-query")) {
			t.Fatalf("cancelled query accepted: %v", err)
		}
	}
}
