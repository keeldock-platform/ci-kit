// wiring-check evaluates public workflow inputs without resolving secret values.
package main

import (
	"fmt"
	"github.com/keeldock-platform/ci-kit/internal/wiring"
	"os"
)

func main() {
	if len(os.Args) != 5 {
		os.Exit(2)
	}
	c, e := os.ReadFile(os.Args[1])
	if e != nil {
		os.Exit(2)
	}
	k, e := os.ReadFile(os.Args[2])
	if e != nil {
		os.Exit(2)
	}
	if wiring.Check(c, k, os.Args[3], os.Args[4]) != nil {
		fmt.Println("FAIL wiring")
		os.Exit(1)
	}
	fmt.Println("PASS wiring")
}
