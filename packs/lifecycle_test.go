package packs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestContainerFailedClientRecoversIDAndCleansOnlyOwnedContainer(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("a", 64)
	cleaned := false
	run := func(ctx context.Context, argv []string, cwd string, timeout time.Duration, env map[string]string) ([]byte, error) {
		if argv[0] == "id" {
			return []byte("1001\n"), nil
		}
		if len(argv) > 1 && argv[1] == "run" {
			for i, value := range argv {
				if value == "--cidfile" {
					if e := os.WriteFile(argv[i+1], []byte(id), 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			return nil, errors.New("client interrupted")
		}
		if len(argv) > 1 && argv[1] == "rm" {
			if len(argv) != 4 || argv[2] != "--force" || argv[3] != id {
				t.Fatal("cleanup targeted a different resource")
			}
			cleaned = true
		}
		return nil, nil
	}
	_, e := containerWithExecutor(context.Background(), "linux-debian", ContainerSupport{Image: images["linux-debian"]}, run, "/usr/bin/docker", filepath.Join(root, "kit"), filepath.Join(root, "source"))
	if e == nil || !cleaned {
		t.Fatal("failed client did not clean recovered owned identity")
	}
}
func TestContainerRefusesUbuntuAsDebianAndStillCleans(t *testing.T) {
	root := t.TempDir()
	id := strings.Repeat("b", 64)
	cleaned := false
	run := func(ctx context.Context, argv []string, cwd string, timeout time.Duration, env map[string]string) ([]byte, error) {
		if argv[0] == "id" {
			return []byte("1001\n"), nil
		}
		if len(argv) > 1 && argv[1] == "run" {
			for i, value := range argv {
				if value == "--cidfile" {
					os.WriteFile(argv[i+1], []byte(id), 0600)
				}
			}
			return []byte(id), nil
		}
		if len(argv) > 1 && argv[1] == "exec" {
			return []byte("ID=ubuntu\nVERSION_ID=24.04\n"), nil
		}
		if len(argv) > 1 && argv[1] == "rm" {
			cleaned = argv[len(argv)-1] == id
		}
		return nil, nil
	}
	_, e := containerWithExecutor(context.Background(), "linux-debian", ContainerSupport{Image: images["linux-debian"]}, run, "/usr/bin/docker", filepath.Join(root, "kit"), filepath.Join(root, "source"))
	if e == nil || !cleaned {
		t.Fatal("wrong operating system admitted or resource leaked")
	}
}
func TestSimulatorSwitchAndCloseTouchOnlyCreatedIDs(t *testing.T) {
	phone := "00000000-0000-4000-8000-000000000001"
	tablet := "00000000-0000-4000-8000-000000000002"
	deleted := map[string]bool{}
	shutdown := map[string]bool{}
	a := &iosAdapter{support: IOSSupport{DeveloperDirectory: "/Applications/Xcode_26.0.app/Contents/Developer"}, created: map[string]bool{phone: true, tablet: true}, started: map[string]bool{}, variants: map[string]string{"phone": phone, "tablet": tablet}}
	a.run = func(ctx context.Context, argv []string, cwd string, timeout time.Duration, env map[string]string) ([]byte, error) {
		if len(argv) < 4 || argv[1] != "simctl" || !a.created[argv[3]] {
			t.Fatal("simulator operation used an unowned identity")
		}
		switch argv[2] {
		case "delete":
			deleted[argv[3]] = true
		case "shutdown":
			shutdown[argv[3]] = true
		}
		return nil, nil
	}
	if a.Before(context.Background(), "phone") != nil || a.Before(context.Background(), "tablet") != nil || a.Close(context.Background()) != nil {
		t.Fatal("owned simulator lifecycle failed")
	}
	if !deleted[phone] || !deleted[tablet] || !shutdown[phone] || !shutdown[tablet] {
		t.Fatal("owned device lifecycle incomplete")
	}
}
