// Package schemas contains the offline schema registry shipped with the CLI.
package schemas

import (
	"embed"
	"encoding/json"
	"errors"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"sync"
)

//go:embed *.json
var Files embed.FS
var once sync.Once
var compiler *jsonschema.Compiler
var initErr error
var validateMu sync.Mutex

type closedLoader struct{}

func (closedLoader) Load(string) (any, error) { return nil, errors.New("external schema refused") }
func Validate(name string, v any) error {
	once.Do(func() {
		compiler = jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		compiler.AssertFormat()
		compiler.UseLoader(closedLoader{})
		es, e := Files.ReadDir(".")
		if e != nil {
			initErr = e
			return
		}
		for _, entry := range es {
			if entry.IsDir() {
				continue
			}
			b, e := Files.ReadFile(entry.Name())
			if e != nil {
				initErr = e
				return
			}
			var doc any
			if e = json.Unmarshal(b, &doc); e != nil {
				initErr = e
				return
			}
			id, _ := doc.(map[string]any)["$id"].(string)
			if id != "" {
				if e = compiler.AddResource(id, doc); e != nil {
					initErr = e
					return
				}
			}
		}
	})
	if initErr != nil {
		return initErr
	}
	validateMu.Lock()
	defer validateMu.Unlock()
	s, e := compiler.Compile(name)
	if e != nil {
		return e
	}
	return s.Validate(v)
}
