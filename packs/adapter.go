package packs

import (
	"context"
	"time"
)

// Execute is supplied by the captured process owner. Every byte from maintained
// tools stays in private captures; only bounded protocol stdout is returned.
type Execute func(context.Context, []string, string, time.Duration, map[string]string) ([]byte, error)

// Adapter prepares one native venue and adapts each argument vector to it.
type Adapter interface {
	Wrap(argv []string, source, cwd string) ([]string, string, map[string]string, error)
	Before(context.Context, string) error
	Close(context.Context) error
}
type hostAdapter struct{ Env map[string]string }

func (a *hostAdapter) Wrap(argv []string, source, cwd string) ([]string, string, map[string]string, error) {
	return argv, cwd, a.Env, nil
}
func (a *hostAdapter) Before(context.Context, string) error { return nil }
func (a *hostAdapter) Close(context.Context) error          { return nil }

// Host leaves the already validated standard native host in charge.
func Host() Adapter { return &hostAdapter{Env: map[string]string{}} }
