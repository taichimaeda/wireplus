package wire

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"go/token"
	"go/types"
	"strings"

	"github.com/awalterschulze/gographviz"
)

// Graph returns a string representation of the given wire.NewSet or wire.Build.
// pattern is a singleton slice containing the pattern of the target package.
// name is the name of the function calling wire.Build.
// format is either "graphviz" or "cytospace".
// Returns graphviz or cytospace data in string.
func Graph(ctx context.Context, wd string, env []string, pattern []string, name string, tags string, format string) (string, []error) {
	pkgs, errs := LoadPackages(ctx, wd, env, tags, pattern)
	if len(errs) > 0 {
		return "", errs
	}
	if len(pkgs) != 1 {
		return "", []error{fmt.Errorf("expected exactly one package")}
	}
	pkg := pkgs[0]

	// Create a graph builder according to the requested format.
	var builder GraphBuilder
	if format == "graphviz" {
		builder = newGraphvizBuilder()
	} else if format == "cytospace" {
		builder = newCytospaceBuilder()
	} else {
		return "", []error{fmt.Errorf("unknown format: %s", format)}
	}

	// Build the graph data for the given wire.NewSet or wire.Build.
	if sol, errs := solveForNewSet(pkg, name); len(errs) == 0 {
		// name corresponds to the variable wire.NewSet is assigned to.
		builder.addInputsForNewSet(sol.missing)
		builder.addOutputs(sol.calls, sol.pset, pkg.Fset)
		builder.addDepsForNewSet(sol.calls, sol.missing, pkg.Fset)
		return builder.String(), nil
	}
	if sol, errs := solveForBuild(pkg, name); len(errs) == 0 {
		// name corresponds to the function that calls wire.Build internally.
		builder.addInputsForBuild(sol.ins)
		builder.addOutputs(sol.calls, sol.pset, pkg.Fset)
		builder.addDepsForBuild(sol.calls, sol.ins, pkg.Fset)
		return builder.String(), nil
	}
	return "", errs
}

type GraphBuilder interface {
	addInputsForNewSet(missing []*types.Type)
	addInputsForBuild(ins []*types.Var)
	addOutputs(calls []call, pset *ProviderSet, fset *token.FileSet)
	addDepsForNewSet(calls []call, missing []*types.Type, fset *token.FileSet)
	addDepsForBuild(calls []call, ins []*types.Var, fset *token.FileSet)

	String() string
}

// inputKey returns a string representation of the node for the input variable given to wire.Build.
func inputKey(input *types.Var) string {
	// Each input is identified by its name and type.
	return input.Name() + "#" + input.Type().String()
}

// callKey returns a string representation of the call in the expanded wire.Build.
func callKey(call *call, fset *token.FileSet) string {
	switch call.kind {
	case valueExpr:
		// Stringify the value expression.
		var buf bytes.Buffer
		writer := bufio.NewWriter(&buf)
		if err := format.Node(writer, fset, call.valueExpr); err != nil {
			panic(err)
		}
		writer.Flush()
		// Each value expression is identified by its string value and type.
		return buf.String() + "#" + call.valueTypeInfo.TypeOf(call.valueExpr).String()
	case funcProviderCall, structProvider, selectorExpr:
		// Each provider call is identified by its name and package path.
		return call.name + "#" + call.pkg.Path()
	}
	panic("unknown kind")
}

func formatKey(key string) string {
	return `"` + strings.Replace(key, "#", `\n`, -1) + `"`
}

// parentKeys the parent provider sets as a slice of keys, from the furtherest to the nearest.
// e.g.
// setA := wire.NewSet(MyProvider)
// setB := wire.NewSet(setA)
// setC := wire.NewSet(setB)
// func NewApplication() { return wire.Build(setC) }
// This gives the following keys:
// ["setC", "setB", "setA"]
func parentKeys(p *providerSetSrc, t *types.Type) []string {
	if p.Import != nil {
		if parent := p.Import.srcMap.At(*t); parent != nil {
			parentKeys := parentKeys(parent.(*providerSetSrc), t)
			key := p.Import.VarName + "#" + p.Import.PkgPath
			if p.Import.VarName == "" {
				key = "<anonymous>" + "#" + p.Import.PkgPath
			}
			// Recursively find the parent provider sets.
			return append([]string{key}, parentKeys...)
		} else {
			return []string{p.Import.PkgPath}
		}
	}
	return []string{}
}

type GraphvizBuilder struct {
	gviz *gographviz.Escape
}

func newGraphvizBuilder() GraphBuilder {
	// Create new gographviz instance with escape support.
	gviz := gographviz.NewEscape()
	// Configure the root graph.
	gviz.SetName("cluster-all")
	gviz.SetDir(true)
	return &GraphvizBuilder{gviz}
}

func (builder *GraphvizBuilder) addInputsForNewSet(missing []*types.Type) {
	for _, m := range missing {
		key := (*m).String()
		label := formatKey(key)
		// Each missing input in wire.NewSet has no dependency and thus becomes a terminating node.
		builder.gviz.AddNode("cluster-all", key, map[string]string{
			"label": label,
			"shape": "octagon",
		})
	}
}

func (builder *GraphvizBuilder) addInputsForBuild(ins []*types.Var) {
	for _, in := range ins {
		key := inputKey(in)
		label := formatKey(key)
		// Each input for wire.Build has no dependency and thus becomes a terminating node.
		builder.gviz.AddNode("cluster-all", key, map[string]string{
			"label": label,
			"shape": "octagon",
		})
	}
}

func (builder *GraphvizBuilder) addOutputs(calls []call, pset *ProviderSet, fset *token.FileSet) {
	// Collect all the calls whose output is used by other calls.
	usedCalls := map[int]bool{}
	for _, call := range calls {
		for _, arg := range call.args {
			usedCalls[arg] = true
		}
	}
	for i, call := range calls {
		// Sort out the subgraph relationships.
		src := pset.srcMap.At(call.out)
		parentKeys := parentKeys(src.(*providerSetSrc), &call.out)
		parentKeys = append([]string{"all"}, parentKeys...)
		for j := range parentKeys {
			if j == 0 {
				// We already created the root graph in the constructor
				continue
			}
			// Create parent subgraphs if not present.
			// The prefix "cluster" is required for grouping nodes in Graphviz
			curKey := "cluster-" + parentKeys[j]
			curLabel := formatKey(curKey)
			parKey := "cluster-" + parentKeys[j-1]
			if !builder.gviz.IsSubGraph(curKey) {
				builder.gviz.AddSubGraph(parKey, curKey, map[string]string{
					"label": curLabel,
					"color": "red",
				})
			}
		}

		// Find information about the current provider.
		key := callKey(&call, fset)
		label := formatKey(key)
		parent := "cluster-" + parentKeys[len(parentKeys)-1]
		// Find the shape for this node.
		var shape string
		if _, ok := usedCalls[i]; !ok {
			// This call is not used and thus becomes a starting node.
			// The output of this call is what wire.Build ultimately returns.
			shape = "doubleoctagon"
		} else {
			// Otherwise it becomes a normal node.
			shape = "box"
		}
		builder.gviz.AddNode(parent, key, map[string]string{
			"label": label,
			"shape": shape,
		})
	}
}

func (builder *GraphvizBuilder) addDepsForNewSet(calls []call, missing []*types.Type, fset *token.FileSet) {
	// Add call dependencies as edges between nodes.
	for _, call := range calls {
		for _, arg := range call.args {
			from := callKey(&call, fset)
			var to string
			if arg >= len(calls) {
				v := missing[arg-len(calls)]
				// Key for missing types in a wire.NewSet is simply the string representation of the type.
				to = (*v).String()
			} else {
				to = callKey(&calls[arg], fset)
			}
			builder.gviz.AddEdge(from, to, true, nil)
		}
	}
}

func (builder *GraphvizBuilder) addDepsForBuild(calls []call, ins []*types.Var, fset *token.FileSet) {
	// Add call dependencies as edges between nodes.
	for _, call := range calls {
		for _, arg := range call.args {
			from := callKey(&call, fset)
			var to string
			if arg < len(ins) {
				to = inputKey(ins[arg])
			} else {
				to = callKey(&calls[arg-len(ins)], fset)
			}
			builder.gviz.AddEdge(from, to, true, nil)
		}
	}
}

func (builder *GraphvizBuilder) String() string {
	return builder.gviz.String()
}

type CytospaceNode struct {
	Data CytospaceNodeData `json:"data"`
}

type CytospaceNodeData struct {
	Id     string  `json:"id"`
	Parent *string `json:"parent"` // optional
	// These are custom fields and are not required by cytospace.
	Content  string `json:"content"`
	Tooltip  string `json:"tooltip"`
	Subgraph bool   `json:"subgraph"`
	Shape    string `json:"shape"`
}

type CytospaceEdge struct {
	Data CytospaceEdgeData `json:"data"`
}

type CytospaceEdgeData struct {
	Id     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type CytospaceElements struct {
	Nodes []CytospaceNode `json:"nodes"`
	Edges []CytospaceEdge `json:"edges"`
}

type CytospaceBuilder struct {
	elems          CytospaceElements
	usedParentKeys map[string]bool // set of already added parent keys
}

func newCytospaceBuilder() GraphBuilder {
	return &CytospaceBuilder{
		elems: CytospaceElements{
			Nodes: []CytospaceNode{},
			Edges: []CytospaceEdge{},
		},
		usedParentKeys: map[string]bool{},
	}
}

func (builder *CytospaceBuilder) addInputsForNewSet(missing []*types.Type) {
	for _, m := range missing {
		key := (*m).String()
		content := strings.Split(key, "#")[0]
		tooltip := strings.Split(key, "#")[1]
		// Each missing input in wire.NewSet has no dependency and thus becomes a terminating node.
		builder.elems.Nodes = append(builder.elems.Nodes, CytospaceNode{
			Data: CytospaceNodeData{
				Id:      key,
				Content: content,
				Tooltip: tooltip,
				Shape:   "octagon",
			},
		})
	}
}

func (builder *CytospaceBuilder) addInputsForBuild(ins []*types.Var) {
	for _, in := range ins {
		key := inputKey(in)
		content := strings.Split(key, "#")[0]
		tooltip := strings.Split(key, "#")[1]
		// Each input for wire.Build has no dependency and thus becomes a terminating node.
		builder.elems.Nodes = append(builder.elems.Nodes, CytospaceNode{
			Data: CytospaceNodeData{
				Id:      key,
				Content: content,
				Tooltip: tooltip,
				Shape:   "octagon",
			},
		})
	}
}

func (builder *CytospaceBuilder) addOutputs(calls []call, pset *ProviderSet, fset *token.FileSet) {
	// Collect all the calls whose output is used by other calls.
	usedCalls := map[int]bool{}
	for _, call := range calls {
		for _, arg := range call.args {
			usedCalls[arg] = true
		}
	}
	for i, call := range calls {
		// Sort out the subgraph relationships.
		src := pset.srcMap.At(call.out)
		parentKeys := parentKeys(src.(*providerSetSrc), &call.out)
		for j := range parentKeys {
			// Create parent subgraphs if not present.
			curKey := parentKeys[j]
			curContent := strings.Split(curKey, "#")[0]
			curTooltip := strings.Split(curKey, "#")[1]
			var parKey *string
			if j > 0 {
				parKey = &parentKeys[j-1]
			}
			if _, ok := builder.usedParentKeys[curKey]; !ok {
				builder.usedParentKeys[curKey] = true
				builder.elems.Nodes = append(builder.elems.Nodes, CytospaceNode{
					Data: CytospaceNodeData{
						Id:       curKey,
						Content:  curContent,
						Tooltip:  curTooltip,
						Subgraph: true,
						Parent:   parKey,
						Shape:    "rectangle",
					},
				})
			}
		}

		// Find information about the current provider.
		key := callKey(&call, fset)
		content := strings.Split(key, "#")[0]
		tooltip := strings.Split(key, "#")[1]
		// Find the parent label for this node.
		var parent *string
		if len(parentKeys) > 0 {
			parent = &parentKeys[len(parentKeys)-1]
		} else {
			parent = nil
		}
		// Find the shape for this node.
		var shape string
		if _, ok := usedCalls[i]; !ok {
			// call is not used and thus becomes a starting node.
			shape = "round-octagon"
		} else {
			// Otherwise becomes a normal node.
			shape = "rectangle"
		}
		builder.elems.Nodes = append(builder.elems.Nodes, CytospaceNode{
			Data: CytospaceNodeData{
				Id:      key,
				Parent:  parent,
				Content: content,
				Tooltip: tooltip,
				Shape:   shape,
			},
		})
	}
}

func (builder *CytospaceBuilder) addDepsForNewSet(calls []call, missing []*types.Type, fset *token.FileSet) {
	// Add call dependencies as edges between nodes.
	for _, call := range calls {
		for _, arg := range call.args {
			from := callKey(&call, fset)
			var to string
			if arg >= len(calls) {
				v := missing[arg-len(calls)]
				// Key for missing types in a wire.NewSet is simply the string representation of the type.
				to = (*v).String()
			} else {
				to = callKey(&calls[arg], fset)
			}
			builder.elems.Edges = append(builder.elems.Edges, CytospaceEdge{
				Data: CytospaceEdgeData{
					Id:     from + "->" + to,
					Source: from,
					Target: to,
				},
			})
		}
	}
}

func (builder *CytospaceBuilder) addDepsForBuild(calls []call, ins []*types.Var, fset *token.FileSet) {
	for _, call := range calls {
		for _, arg := range call.args {
			from := callKey(&call, fset)
			var to string
			if arg < len(ins) {
				to = inputKey(ins[arg])
			} else {
				to = callKey(&calls[arg-len(ins)], fset)
			}
			builder.elems.Edges = append(builder.elems.Edges, CytospaceEdge{
				Data: CytospaceEdgeData{
					Id:     from + "->" + to,
					Source: from,
					Target: to,
				},
			})
		}
	}
}

func (builder *CytospaceBuilder) String() string {
	bytes, _ := json.Marshal(builder.elems)
	return string(bytes)
}
