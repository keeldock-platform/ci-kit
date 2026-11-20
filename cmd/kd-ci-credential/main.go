// This private-protocol helper is invoked by trusted Git acquisition only.
package main

import (
	"github.com/keeldock-platform/ci-kit/internal/acquire"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(1)
	}
	switch os.Args[1] {
	case "get":
		if acquire.ServeCredential(os.Stdin, os.Stdout, os.Getenv) != nil {
			os.Exit(1)
		}
	case "store", "erase":
	default:
		os.Exit(1)
	}
}
