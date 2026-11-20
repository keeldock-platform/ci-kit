package catalog

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

// Alias is private aggregate metadata, distinct from a feature's own alias.
type Alias struct {
	ID    string
	Units []string
}

var aliasID = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,95}$`)

// AggregateAliases validates and expands aggregate aliases through a maintained DAG.
func AggregateAliases(units []Unit, shared Shared) ([]Alias, error) {
	entries, e := Entries(units)
	if e != nil {
		return nil, e
	}
	known := map[string]bool{}
	for _, u := range units {
		known[u.ID] = true
	}
	for _, entry := range entries {
		known[entry.ID] = true
		if entry.Shard != "" {
			known[entry.ID+"#"+entry.Shard] = true
		}
	}
	if len(shared.Aliases) > 10000 {
		return nil, ErrInvalid
	}
	names := make([]string, 0, len(shared.Aliases))
	for name, targets := range shared.Aliases {
		if !aliasID.MatchString(name) || known[name] || len(targets) == 0 || len(targets) > 256 {
			return nil, ErrInvalid
		}
		names = append(names, name)
	}
	sort.Strings(names)
	graph := simple.NewDirectedGraph()
	nodes := map[string]int64{}
	for i, name := range names {
		nodes[name] = int64(i)
		graph.AddNode(simple.Node(i))
	}
	for name, targets := range shared.Aliases {
		seen := map[string]bool{}
		for _, target := range targets {
			if seen[target] {
				return nil, ErrInvalid
			}
			seen[target] = true
			if target == name {
				return nil, ErrInvalid
			}
			if _, ok := shared.Aliases[target]; ok {
				graph.SetEdge(graph.NewEdge(simple.Node(nodes[target]), simple.Node(nodes[name])))
			} else if !known[target] {
				return nil, ErrInvalid
			}
		}
	}
	ordered, e := topo.SortStabilized(graph, nil)
	if e != nil {
		return nil, ErrInvalid
	}
	expanded := map[string][]string{}
	for _, node := range ordered {
		name := names[node.ID()]
		values := []string{}
		for _, target := range shared.Aliases[name] {
			if resolved, ok := expanded[target]; ok {
				values = append(values, resolved...)
			} else {
				values = append(values, target)
			}
			if len(values) > 100000 {
				return nil, ErrInvalid
			}
		}
		sort.Strings(values)
		expanded[name] = slices.CompactFunc(values, strings.EqualFold)
	}
	result := make([]Alias, 0, len(names))
	for _, name := range names {
		result = append(result, Alias{ID: name, Units: expanded[name]})
	}
	return result, nil
}

// ExpandAliases resolves private convenience names before normal exact selection.
func ExpandAliases(units []Unit, shared Shared, selectors []string) ([]string, error) {
	aliases, e := AggregateAliases(units, shared)
	if e != nil {
		return nil, e
	}
	expanded := map[string][]string{}
	for _, alias := range aliases {
		expanded[alias.ID] = alias.Units
	}
	known := map[string]bool{}
	for _, u := range units {
		known[u.ID] = true
	}
	entries, e := Entries(units)
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		known[entry.ID] = true
		if entry.Shard != "" {
			known[entry.ID+"#"+entry.Shard] = true
		}
	}
	out := []string{}
	for _, selector := range selectors {
		if values, ok := expanded[selector]; ok {
			out = append(out, values...)
		} else if known[selector] {
			out = append(out, selector)
		} else {
			return nil, ErrInvalid
		}
		if len(out) > 100000 {
			return nil, ErrInvalid
		}
	}
	sort.Strings(out)
	return slices.CompactFunc(out, strings.EqualFold), nil
}
