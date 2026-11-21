package packs

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFirefoxArchiveSafety(t *testing.T) {
	for _, name := range []string{"firefox/firefox", "../firefox/firefox", "/firefox/firefox", "firefox/../firefox", "firefox/./firefox", `firefox\firefox`, "firefox/file:stream", "other/firefox"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, "browser.zip")
			file, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(file)
			h := &zip.FileHeader{Name: name, Method: zip.Deflate}
			h.SetMode(0755)
			w, err := z.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			w.Write([]byte("public fixture"))
			z.Close()
			file.Close()
			err = extractFirefox(context.Background(), archive, filepath.Join(root, "out"))
			if (name == "firefox/firefox") != (err == nil) {
				t.Fatalf("archive path result: %q: %v", name, err)
			}
		})
	}
	for _, mode := range []os.FileMode{os.ModeSymlink | 0777, os.ModeNamedPipe | 0600} {
		root := t.TempDir()
		file, _ := os.Create(filepath.Join(root, "browser.zip"))
		z := zip.NewWriter(file)
		h := &zip.FileHeader{Name: "firefox/firefox"}
		h.SetMode(mode)
		w, _ := z.CreateHeader(h)
		w.Write([]byte("outside"))
		z.Close()
		file.Close()
		if extractFirefox(context.Background(), file.Name(), filepath.Join(root, "out")) == nil {
			t.Fatal("unsafe archive mode accepted")
		}
	}
}

// This opt-in source-free hosted regression exercises the actual container
// adapter, fixed Firefox binary, axe and both ARIA reflection world-read orders.
func TestBrowserRuntime(t *testing.T) {
	if os.Getenv("CI_PUBLIC_BROWSER_TEST") != "1" {
		t.Skip("requires hosted public Docker experiment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if os.Mkdir(source, 0700) != nil {
		t.Fatal("fixture directory")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	kit := filepath.Dir(wd)
	run := func(ctx context.Context, argv []string, cwd string, timeout time.Duration, env map[string]string) ([]byte, error) {
		cctx, stop := context.WithTimeout(ctx, timeout)
		defer stop()
		c := exec.CommandContext(cctx, argv[0], argv[1:]...)
		c.Dir = cwd
		c.Env = os.Environ()
		for k, v := range env {
			c.Env = append(c.Env, k+"="+v)
		}
		b, e := c.CombinedOutput()
		t.Logf("public command %s: %s", filepath.Base(argv[0]), b)
		return b, e
	}
	adapter, err := Container(ctx, "web-linux", ContainerSupport{Image: images["web-linux"], PlaywrightVersion: "1.65.1"}, run, "/usr/bin/docker", kit, source)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if adapter.Close(context.Background()) != nil {
			t.Error("container cleanup")
		}
	}()
	script := `const {firefox,chromium,webkit}=require('playwright');const AxeBuilder=require('@axe-core/playwright').default;const assert=require('node:assert/strict');
(async()=>{for(const [name,type] of Object.entries({firefox,chromium,webkit})){const b=await type.launch({headless:true});try{console.log('ENGINE',name,b.version());for(const order of ['role-first','axe-first','script-set']){const c=await b.newContext();try{const p=await c.newPage();await p.setContent('<!doctype html><html lang="en"><head><title>Public fixture</title></head><body><main><section aria-labelledby="public-title"><h1 id="public-title">Public fixture</h1><label for="public-input">Public input</label><input id="public-input"><button>Continue</button></section></main></body></html>');const role=()=>p.getByRole('region',{name:'Public fixture'}).count();if(order==='script-set'){await p.evaluate(()=>{const region=document.querySelector('section');region.removeAttribute('aria-labelledby');region.ariaLabelledByElements=[document.querySelector('h1')];void region.ariaLabelledByElements;});}if(order!=='axe-first')assert.equal(await role(),1);const result=await new AxeBuilder({page:p}).analyze();assert.equal(result.violations.length,0);assert.equal(await role(),1);await p.getByLabel('Public input').fill('public');await p.getByRole('button',{name:'Continue'}).click();console.log('PASS',name,order);}finally{await c.close();}}}finally{await b.close();}}})().catch(e=>{console.error(e);process.exitCode=1;});`
	if os.WriteFile(filepath.Join(source, "probe.cjs"), []byte(script), 0600) != nil {
		t.Fatal("public script")
	}
	for _, argv := range [][]string{{"npm", "install", "--ignore-scripts", "--no-audit", "--no-fund", "playwright@1.65.1", "@axe-core/playwright@4.14.0"}, {"node", filepath.Join(kit, "packs", "web-linux", "readiness.cjs")}, {"node", "probe.cjs"}} {
		// The kit is mounted read-only at /public-control inside the container.
		if argv[0] == "node" && strings.HasSuffix(argv[len(argv)-1], "readiness.cjs") {
			argv[1] = "/public-control/packs/web-linux/readiness.cjs"
		}
		args, cwd, env, e := adapter.Wrap(argv, source, ".")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = run(ctx, args, cwd, 3*time.Minute, env); e != nil {
			t.Fatal(fmt.Errorf("public browser regression failed: %w", e))
		}
	}
}

type publicZeros struct{}

func (publicZeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }
func TestFirefoxDownloadRefusesMismatchTruncationAndOverflow(t *testing.T) {
	for _, reader := range []io.Reader{strings.NewReader("changed browser"), strings.NewReader("PK"), io.LimitReader(publicZeros{}, maxFirefoxArchive+1)} {
		if receiveFirefox(io.Discard, reader) == nil {
			t.Fatal("unverified browser accepted")
		}
	}
}
func TestFirefoxPreparationPreservesPreexistingDirectory(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, ".public-browsers")
	if os.Mkdir(existing, 0700) != nil {
		t.Fatal("directory")
	}
	sentinel := filepath.Join(existing, "sentinel")
	os.WriteFile(sentinel, []byte("keep"), 0600)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareFirefox(ctx, root, nil); err == nil {
		t.Fatal("preexisting overlay accepted")
	}
	if b, err := os.ReadFile(sentinel); err != nil || string(b) != "keep" {
		t.Fatal("existing directory changed")
	}
}
func TestFirefoxArchiveRefusesDuplicateOversizedAndNonExecutable(t *testing.T) {
	for _, scenario := range []string{"duplicate", "oversize", "nonexec", "missing", "cancelled"} {
		root := t.TempDir()
		file, _ := os.Create(filepath.Join(root, "browser.zip"))
		z := zip.NewWriter(file)
		h := &zip.FileHeader{Name: "firefox/firefox", Method: zip.Store}
		h.SetMode(0755)
		if scenario == "missing" {
			h.Name = "firefox/readme"
		}
		if scenario == "nonexec" {
			h.SetMode(0644)
		}
		if scenario == "oversize" {
			h.UncompressedSize64 = maxFirefoxExpanded + 1
			w, _ := z.CreateRaw(h)
			w.Write([]byte("x"))
		} else {
			w, _ := z.CreateHeader(h)
			w.Write([]byte("x"))
			if scenario == "duplicate" {
				duplicate := *h
				var err error
				w, err = z.CreateHeader(&duplicate)
				if err != nil {
					t.Fatal(err)
				}
				w.Write([]byte("x"))
			}
		}
		z.Close()
		file.Close()
		ctx := context.Background()
		if scenario == "cancelled" {
			c, stop := context.WithCancel(ctx)
			stop()
			ctx = c
		}
		if extractFirefox(ctx, file.Name(), filepath.Join(root, "out")) == nil {
			t.Fatalf("unsafe archive accepted: %s", scenario)
		}
	}
}
