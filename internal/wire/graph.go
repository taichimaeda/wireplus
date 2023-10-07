package wire

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/types"

	"github.com/awalterschulze/gographviz"
	"golang.org/x/tools/go/packages"
)

// TODO: Move this function to the bottom of this file.
func Graph(ctx context.Context, wd string, env []string, pattern string, name string, tags string) (*gographviz.Graph, []error) {
	pkgs, errs := load(ctx, wd, env, tags, []string{pattern})
	if len(errs) > 0 {
		return nil, errs
	}
	if len(pkgs) != 1 {
		return nil, []error{fmt.Errorf("expected exactly one package")}
	}
	pkg := pkgs[0]
	v := newGrapher(pkg)
	if errs := v.generateInjector(name); errs != nil {
		return nil, errs
	}
	// Create new graph with escape
	graph := gographviz.NewEscape()
	graph.SetName("cluster-all")
	graph.SetDir(true)
	// Add given arguments as nodes
	for _, in := range v.ins {
		key := v.givenIdent(in, "#")
		label := v.givenIdent(in, `\n`)
		graph.AddNode("cluster-all", key, map[string]string{
			"label": `"` + label + `"`,
			"shape": "octagon",
		})
	}
	// Add providers in the injection calls as nodes
	for i, call := range v.calls {
		// Sort out the subgraph relationships
		sets := v.collectSetLabels(v.pset.srcMap.At(call.out).(*providerSetSrc), &call.out)
		sets = append([]string{"all"}, sets...)
		for j := range sets {
			if j == 0 {
				continue
			}
			// cluster prefix is required for grouping nodes in Graphviz
			cur := "cluster-" + sets[j]
			par := "cluster-" + sets[j-1]
			if !graph.IsSubGraph(cur) {
				graph.AddSubGraph(par, cur, map[string]string{
					"label": sets[j],
					"color": "red",
				})
			}
		}
		// Find the current provider
		parent := "cluster-" + sets[len(sets)-1]
		from := v.callIdent(&call, "#")
		label := v.callIdent(&call, `\n`)
		graph.AddNode(parent, from, map[string]string{
			"label": `"` + label + `"`,
			"shape": map[bool]string{true: "box", false: "doubleoctagon"}[i < len(v.calls)-1],
		})
		// Add dependencies as edges
		for _, arg := range call.args {
			if arg < len(v.ins) {
				to := v.givenIdent(v.ins[arg], "#")
				graph.AddEdge(from, to, true, nil)
			} else {
				to := v.callIdent(&v.calls[arg-len(v.ins)], "#")
				graph.AddEdge(from, to, true, nil)
			}
		}
	}
	return graph.Graph, nil
}

type grapher struct {
	pkg   *packages.Package
	calls []call
	ins   []*types.Var
	pset  *ProviderSet
}

func newGrapher(pkg *packages.Package) *grapher {
	return &grapher{
		pkg: pkg,
	}
}

func (g *grapher) callIdent(call *call, delim string) string {
	switch call.kind {
	case valueExpr:
		return g.formatExpr(&call.valueExpr) + delim + call.valueTypeInfo.TypeOf(call.valueExpr).String()
	case funcProviderCall, structProvider, selectorExpr:
		return call.name + delim + call.pkg.Path()
	}
	panic("unknown kind")
}

func (g *grapher) givenIdent(given *types.Var, delim string) string {
	return given.Name() + delim + given.Type().String()
}

func (g *grapher) formatExpr(expr *ast.Expr) string {
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	if err := format.Node(writer, g.pkg.Fset, *expr); err != nil {
		panic(err)
	}
	writer.Flush()
	return buf.String()
}

func (g *grapher) generateInjector(name string) []error {
	fn, err := findFuncDecl(g.pkg, name)
	if err != nil {
		return []error{err}
	}
	buildCall, err := findInjectorBuild(g.pkg.TypesInfo, fn)
	if err != nil {
		return []error{err}
	}
	if buildCall == nil {
		return []error{fmt.Errorf("no injector build call found")}
	}
	sig := g.pkg.TypesInfo.ObjectOf(fn.Name).Type().(*types.Signature)
	ins, out, err := injectorFuncSignature(sig)
	if err != nil {
		if w, ok := err.(*wireErr); ok {
			return []error{notePosition(w.position, fmt.Errorf("inject %s: %v", fn.Name.Name, w.error))}
		} else {
			return []error{notePosition(g.pkg.Fset.Position(fn.Pos()), fmt.Errorf("inject %s: %v", fn.Name.Name, err))}
		}
	}
	injectorArgs := &InjectorArgs{
		Name:  fn.Name.Name,
		Tuple: ins,
		Pos:   fn.Pos(),
	}
	oc := newObjectCache([]*packages.Package{g.pkg})
	set, errs := oc.processNewSet(g.pkg.TypesInfo, g.pkg.PkgPath, buildCall, injectorArgs, "")
	if len(errs) > 0 {
		return notePositionAll(g.pkg.Fset.Position(fn.Pos()), errs)
	}
	params := sig.Params()
	calls, errs := solve(g.pkg.Fset, out.out, params, set)
	if len(errs) > 0 {
		return mapErrors(errs, func(e error) error {
			if w, ok := e.(*wireErr); ok {
				return notePosition(w.position, fmt.Errorf("inject %s: %v", name, w.error))
			}
			return notePosition(g.pkg.Fset.Position(fn.Pos()), fmt.Errorf("inject %s: %v", name, e))
		})
	}
	g.calls = calls
	for i := 0; i < ins.Len(); i++ {
		g.ins = append(g.ins, ins.At(i))
	}
	g.pset = set
	return nil
}

func findFuncDecl(pkg *packages.Package, name string) (*ast.FuncDecl, error) {
	for _, f := range pkg.Syntax {
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				if fn.Name.Name == name {
					return fn, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("function %s not found in %s", name, pkg.PkgPath)
}

func (g *grapher) collectSetLabels(p *providerSetSrc, t *types.Type) []string {
	if p.Import != nil {
		if parent := p.Import.srcMap.At(*t); parent != nil {
			sets := g.collectSetLabels(parent.(*providerSetSrc), t)
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
