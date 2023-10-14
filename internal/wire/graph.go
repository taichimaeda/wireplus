package wire

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go/format"
	"go/token"
	"go/types"

	"github.com/awalterschulze/gographviz"
)

type Graphviz = gographviz.Escape

// pattern is the pattern of the target package (must be a singleton).
// name is the name of the function calling wire.Build.
func Graph(ctx context.Context, wd string, env []string, pattern []string, name string, tags string) (*Graphviz, []error) {
	// Create new gviz with escape
	gviz := gographviz.NewEscape()
	gviz.SetName("cluster-all")
	gviz.SetDir(true)

	pkgs, errs := LoadPackages(ctx, wd, env, tags, pattern)
	if len(errs) > 0 {
		return nil, errs
	}
	if len(pkgs) != 1 {
		return nil, []error{fmt.Errorf("expected exactly one package")}
	}
	pkg := pkgs[0]
	fn := findFuncDeclByName(pkg, name)
	set := findVarExprByName(pkg, name)

	switch {
	case set != nil:
		sol, errs := solveForNewSet(pkg, name)
		if len(errs) > 0 {
			return nil, errs
		}
		addInputsForNewSet(gviz, sol.missing)
		addOutputs(gviz, sol.calls, sol.pset, pkg.Fset)
		addDepsForNewSet(gviz, sol.calls, sol.missing, pkg.Fset)
	case fn != nil:
		sol, errs := solveForBuild(pkg, name)
		if len(errs) > 0 {
			return nil, errs
		}
		addInputsForBuild(gviz, sol.ins)
		addOutputs(gviz, sol.calls, sol.pset, pkg.Fset)
		addDepsForBuild(gviz, sol.calls, sol.ins, pkg.Fset)
	default:
		return nil, []error{fmt.Errorf("no function or variable named %q found", name)}
	}
	return gviz, nil
}

func addInputsForNewSet(gviz *Graphviz, missing []*types.Type) {
	for _, m := range missing {
		key := (*m).String()
		label := escapeLabel((*m).String())
		// m has no dependency and thus becomes a terminating node.
		gviz.AddNode("cluster-all", key, map[string]string{
			"label": label,
			"shape": "octagon",
		})
	}
}

func addInputsForBuild(gviz *Graphviz, ins []*types.Var) {
	for _, in := range ins {
		key := givenDisplayName(in, "#")
		label := escapeLabel(givenDisplayName(in, `\n`))
		// in has no dependency and thus becomes a terminating node.
		gviz.AddNode("cluster-all", key, map[string]string{
			"label": label,
			"shape": "octagon",
		})
	}
}

func addOutputs(gviz *Graphviz, calls []call, pset *ProviderSet, fset *token.FileSet) {
	used := map[int]bool{}
	for _, call := range calls {
		for _, arg := range call.args {
			used[arg] = true
		}
	}
	for i, call := range calls {
		// Sort out the subgraph relationships
		src := pset.srcMap.At(call.out)
		labels := collectParentLabels(src.(*providerSetSrc), &call.out)
		labels = append([]string{"all"}, labels...)
		for j := range labels {
			if j == 0 {
				continue
			}
			// cluster prefix is required for grouping nodes in Graphviz
			cur := "cluster-" + labels[j]
			par := "cluster-" + labels[j-1]
			if !gviz.IsSubGraph(cur) {
				gviz.AddSubGraph(par, cur, map[string]string{
					"label": labels[j],
					"color": "red",
				})
			}
		}
		// Find the current provider
		parent := "cluster-" + labels[len(labels)-1]
		key := callDisplayName(&call, "#", fset)
		label := escapeLabel(callDisplayName(&call, `\n`, fset))
		var shape string
		if _, ok := used[i]; !ok {
			// call is not used and thus becomes a starting node.
			shape = "doubleoctagon"
		} else {
			// Otherwise becomes a normal node.
			shape = "box"
		}
		gviz.AddNode(parent, key, map[string]string{
			"label": label,
			"shape": shape,
		})
	}
}

func addDepsForNewSet(gviz *Graphviz, calls []call, missing []*types.Type, fset *token.FileSet) {
	// Add dependencies as edges
	for _, call := range calls {
		for _, arg := range call.args {
			from := callDisplayName(&call, "#", fset)
			var to string
			if arg >= len(calls) {
				v := missing[arg-len(calls)]
				to = (*v).String()
			} else {
				to = callDisplayName(&calls[arg], "#", fset)
			}
			gviz.AddEdge(from, to, true, nil)
		}
	}
}

func addDepsForBuild(gviz *Graphviz, calls []call, ins []*types.Var, fset *token.FileSet) {
	// Add dependencies as edges
	for _, call := range calls {
		for _, arg := range call.args {
			from := callDisplayName(&call, "#", fset)
			if arg < len(ins) {
				to := givenDisplayName(ins[arg], "#")
				gviz.AddEdge(from, to, true, nil)
			} else {
				to := callDisplayName(&calls[arg-len(ins)], "#", fset)
				gviz.AddEdge(from, to, true, nil)
			}
		}
	}
}

func escapeLabel(label string) string {
	return `"` + label + `"`
}

func givenDisplayName(given *types.Var, delim string) string {
	return given.Name() + delim + given.Type().String()
}

func callDisplayName(call *call, delim string, fset *token.FileSet) string {
	switch call.kind {
	case valueExpr:
		var buf bytes.Buffer
		writer := bufio.NewWriter(&buf)
		if err := format.Node(writer, fset, call.valueExpr); err != nil {
			panic(err)
		}
		writer.Flush()
		return buf.String() + delim + call.valueTypeInfo.TypeOf(call.valueExpr).String()
	case funcProviderCall, structProvider, selectorExpr:
		return call.name + delim + call.pkg.Path()
	}
	panic("unknown kind")
}

func collectParentLabels(p *providerSetSrc, t *types.Type) []string {
	if p.Import != nil {
		if parent := p.Import.srcMap.At(*t); parent != nil {
			sets := collectParentLabels(parent.(*providerSetSrc), t)
			label := p.Import.PkgPath + "#" + p.Import.VarName
			if p.Import.VarName == "" {
				label += "<anonymous>"
			}
			return append([]string{label}, sets...)
		} else {
			return []string{p.Import.PkgPath}
		}
	}
	return []string{}
}
