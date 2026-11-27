// ci-kit is the fixed hosted entry point. Request values arrive through the
// workflow environment, never through dynamically constructed command lines.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/keeldock-platform/ci-kit/internal/acquire"
)

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "get" || os.Args[1] == "store" || os.Args[1] == "erase") {
		if os.Args[1] == "get" && acquire.ServeCredential(os.Stdin, os.Stdout, os.Getenv) != nil {
			os.Exit(3)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := 6

	if len(os.Args) == 2 && os.Args[1] == "native-preflight" {
		code = nativePreflight(ctx)
	}
	if len(os.Args) == 2 && os.Args[1] == "native-hook" {
		code = nativeHook(ctx)
	}
	if len(os.Args) == 3 && os.Args[1] == "tool-stage" { code = lockTools(ctx,os.Args[2]) }
	if len(os.Args) == 2 && os.Args[1] == "action-lock-generate" { code = generateActionLock(ctx) }
	if len(os.Args) == 1 {
		code = execute(ctx)
	}
	if code == 0 {
		fmt.Println("PASS")
	} else {
		fmt.Println("FAIL")
	}
	os.Exit(code)
}
