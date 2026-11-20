package main

import (
	"github.com/keeldock-platform/ci-kit/internal/wiring"
	"os"
)

func checkWiring(args []string) int {
	if len(args) != 4 && len(args) != 5 {
		return 2
	}
	c, e := os.ReadFile(args[0])
	if e != nil {
		return 2
	}
	k, e := os.ReadFile(args[1])
	if e != nil {
		return 2
	}
	if wiring.Check(c, k, args[2], args[3], args[4:]...) != nil {
		return 1
	}
	return 0
}
