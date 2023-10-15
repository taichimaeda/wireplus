// Copyright 2018 The Wire Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Wire is a compile-time dependency injection tool.
//
// For an overview, see https://github.com/google/wire/blob/master/README.md
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/google/subcommands"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/taichimaeda/wireplus/internal/wire"
	"github.com/taichimaeda/wireplus/internal/wire/lsp"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/types/typeutil"
)

func main() {
	subcommands.Register(subcommands.CommandsCommand(), "")
	subcommands.Register(subcommands.FlagsCommand(), "")
	subcommands.Register(subcommands.HelpCommand(), "")
	subcommands.Register(&checkCmd{}, "")
	subcommands.Register(&diffCmd{}, "")
	subcommands.Register(&genCmd{}, "")
	subcommands.Register(&showCmd{}, "")
	subcommands.Register(&detailCmd{}, "")
	subcommands.Register(&graphCmd{}, "")
	subcommands.Register(&lspCmd{}, "")
	flag.Parse()

	// Initialize the default logger to log to stderr.
	log.SetFlags(0)
	log.SetPrefix("wire: ")
	log.SetOutput(os.Stderr)

	// TODO(rvangent): Use subcommands's VisitCommands instead of hardcoded map,
	// once there is a release that contains it:
	// allCmds := map[string]bool{}
	// subcommands.DefaultCommander.VisitCommands(func(_ *subcommands.CommandGroup, cmd subcommands.Command) { allCmds[cmd.Name()] = true })
	allCmds := map[string]bool{
		"commands": true, // builtin
		"help":     true, // builtin
		"flags":    true, // builtin
		"check":    true,
		"diff":     true,
		"gen":      true,
		"show":     true,
		"detail":   true,
		"graph":    true,
		"lsp":      true,
	}
	// Default to running the "gen" command.
	if args := flag.Args(); len(args) == 0 || !allCmds[args[0]] {
		genCmd := &genCmd{}
		os.Exit(int(genCmd.Execute(context.Background(), flag.CommandLine)))
	}
	os.Exit(int(subcommands.Execute(context.Background())))
}

// packages returns the slice of packages to run wire over based on f.
// It defaults to ".".
func packages(f *flag.FlagSet) []string {
	pkgs := f.Args()
	if len(pkgs) == 0 {
		pkgs = []string{"."}
	}
	return pkgs
}

// newGenerateOptions returns an initialized wire.GenerateOptions, possibly
// with the Header option set.
func newGenerateOptions(headerFile string) (*wire.GenerateOptions, error) {
	opts := new(wire.GenerateOptions)
	if headerFile != "" {
		var err error
		opts.Header, err = ioutil.ReadFile(headerFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read header file %q: %v", headerFile, err)
		}
	}
	return opts, nil
}

type genCmd struct {
	headerFile     string
	prefixFileName string
	tags           string
}

func (*genCmd) Name() string { return "gen" }
func (*genCmd) Synopsis() string {
	return "generate the wire_gen.go file for each package"
}
func (*genCmd) Usage() string {
	return `gen [packages]

  Given one or more packages, gen creates the wire_gen.go file for each.

  If no packages are listed, it defaults to ".".
`
}
func (cmd *genCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.headerFile, "header_file", "", "path to file to insert as a header in wire_gen.go")
	f.StringVar(&cmd.prefixFileName, "output_file_prefix", "", "string to prepend to output file names.")
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
}

func (cmd *genCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return subcommands.ExitFailure
	}
	opts, err := newGenerateOptions(cmd.headerFile)
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}

	opts.PrefixOutputFile = cmd.prefixFileName
	opts.Tags = cmd.tags

	outs, errs := wire.Generate(ctx, wd, os.Environ(), packages(f), opts)
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("generate failed")
		return subcommands.ExitFailure
	}
	if len(outs) == 0 {
		return subcommands.ExitSuccess
	}
	success := true
	for _, out := range outs {
		if len(out.Errs) > 0 {
			logErrors(out.Errs)
			log.Printf("%s: generate failed\n", out.PkgPath)
			success = false
		}
		if len(out.Content) == 0 {
			// No Wire output. Maybe errors, maybe no Wire directives.
			continue
		}
		if err := out.Commit(); err == nil {
			log.Printf("%s: wrote %s\n", out.PkgPath, out.OutputPath)
		} else {
			log.Printf("%s: failed to write %s: %v\n", out.PkgPath, out.OutputPath, err)
			success = false
		}
	}
	if !success {
		log.Println("at least one generate failure")
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

type diffCmd struct {
	headerFile string
	tags       string
}

func (*diffCmd) Name() string { return "diff" }
func (*diffCmd) Synopsis() string {
	return "output a diff between existing wire_gen.go files and what gen would generate"
}
func (*diffCmd) Usage() string {
	return `diff [packages]

  Given one or more packages, diff generates the content for their wire_gen.go
  files and outputs the diff against the existing files.

  If no packages are listed, it defaults to ".".

  Similar to the diff command, it returns 0 if no diff, 1 if different, 2
  plus an error if trouble.
`
}
func (cmd *diffCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.headerFile, "header_file", "", "path to file to insert as a header in wire_gen.go")
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
}
func (cmd *diffCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	const (
		errReturn  = subcommands.ExitStatus(2)
		diffReturn = subcommands.ExitStatus(1)
	)
	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return errReturn
	}
	opts, err := newGenerateOptions(cmd.headerFile)
	if err != nil {
		log.Println(err)
		return subcommands.ExitFailure
	}

	opts.Tags = cmd.tags

	outs, errs := wire.Generate(ctx, wd, os.Environ(), packages(f), opts)
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("generate failed")
		return errReturn
	}
	if len(outs) == 0 {
		return subcommands.ExitSuccess
	}
	success := true
	hadDiff := false
	for _, out := range outs {
		if len(out.Errs) > 0 {
			logErrors(out.Errs)
			log.Printf("%s: generate failed\n", out.PkgPath)
			success = false
		}
		if len(out.Content) == 0 {
			// No Wire output. Maybe errors, maybe no Wire directives.
			continue
		}
		// Assumes the current file is empty if we can't read it.
		cur, _ := ioutil.ReadFile(out.OutputPath)
		if diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
			A: difflib.SplitLines(string(cur)),
			B: difflib.SplitLines(string(out.Content)),
		}); err == nil {
			if diff != "" {
				// Print the actual diff to stdout, not stderr.
				fmt.Printf("%s: diff from %s:\n%s\n", out.PkgPath, out.OutputPath, diff)
				hadDiff = true
			}
		} else {
			log.Printf("%s: failed to diff %s: %v\n", out.PkgPath, out.OutputPath, err)
			success = false
		}
	}
	if !success {
		log.Println("at least one generate failure")
		return errReturn
	}
	if hadDiff {
		return diffReturn
	}
	return subcommands.ExitSuccess
}

type showCmd struct {
	tags string
}

func (*showCmd) Name() string { return "show" }
func (*showCmd) Synopsis() string {
	return "describe all top-level provider sets"
}
func (*showCmd) Usage() string {
	return `show [packages]

  Given one or more packages, show finds all the provider sets declared as
  top-level variables and prints what other provider sets they import and what
  outputs they can produce, given possible inputs. It also lists any injector
  functions defined in the package.

  If no packages are listed, it defaults to ".".
`
}
func (cmd *showCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
}
func (cmd *showCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return subcommands.ExitFailure
	}
	info, errs := wire.Load(ctx, wd, os.Environ(), cmd.tags, packages(f))
	if info != nil {
		keys := make([]wire.ProviderSetID, 0, len(info.Sets))
		for k := range info.Sets {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].ImportPath == keys[j].ImportPath {
				return keys[i].VarName < keys[j].VarName
			}
			return keys[i].ImportPath < keys[j].ImportPath
		})
		for i, k := range keys {
			if i > 0 {
				fmt.Println()
			}
			outGroups, imports := gather(info, k)
			fmt.Println(k)
			for _, imp := range sortSet(imports) {
				fmt.Printf("\t%s\n", imp)
			}
			for i := range outGroups {
				fmt.Printf("\tOutputs given %s:\n", outGroups[i].name)
				out := make(map[string]token.Pos, outGroups[i].outputs.Len())
				outGroups[i].outputs.Iterate(func(t types.Type, v interface{}) {
					switch v := v.(type) {
					case *wire.Provider:
						out[types.TypeString(t, nil)] = v.Pos
					case *wire.Value:
						out[types.TypeString(t, nil)] = v.Pos
					case *wire.Field:
						out[types.TypeString(t, nil)] = v.Pos
					default:
						panic("unreachable")
					}
				})
				for _, t := range sortSet(out) {
					fmt.Printf("\t\t%s\n", t)
					fmt.Printf("\t\t\tat %v\n", info.Fset.Position(out[t]))
				}
			}
		}
		if len(info.Injectors) > 0 {
			injectors := append([]*wire.Injector(nil), info.Injectors...)
			sort.Slice(injectors, func(i, j int) bool {
				if injectors[i].ImportPath == injectors[j].ImportPath {
					return injectors[i].FuncName < injectors[j].FuncName
				}
				return injectors[i].ImportPath < injectors[j].ImportPath
			})
			fmt.Println("\nInjectors:")
			for _, in := range injectors {
				fmt.Printf("\t%v\n", in)
			}
		}
	}
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("error loading packages")
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

type checkCmd struct {
	tags string
}

func (*checkCmd) Name() string { return "check" }
func (*checkCmd) Synopsis() string {
	return "print any Wire errors found"
}
func (*checkCmd) Usage() string {
	return `check [-tags tag,list] [packages]

  Given one or more packages, check prints any type-checking or Wire errors
  found with top-level variable provider sets or injector functions.

  If no packages are listed, it defaults to ".".
`
}
func (cmd *checkCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
}
func (cmd *checkCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return subcommands.ExitFailure
	}
	_, errs := wire.Load(ctx, wd, os.Environ(), cmd.tags, packages(f))
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("error loading packages")
		return subcommands.ExitFailure
	}
	return subcommands.ExitSuccess
}

type outGroup struct {
	name    string
	inputs  *typeutil.Map // values are not important
	outputs *typeutil.Map // values are *wire.Provider, *wire.Value, or *wire.Field
}

// gather flattens a provider set into outputs grouped by the inputs
// required to create them. As it flattens the provider set, it records
// the visited named provider sets as imports.
func gather(info *wire.Info, key wire.ProviderSetID) (_ []outGroup, imports map[string]struct{}) {
	set := info.Sets[key]
	hash := typeutil.MakeHasher()

	// Find imports.
	next := []*wire.ProviderSet{info.Sets[key]}
	visited := make(map[*wire.ProviderSet]struct{})
	imports = make(map[string]struct{})
	for len(next) > 0 {
		curr := next[len(next)-1]
		next = next[:len(next)-1]
		if _, found := visited[curr]; found {
			continue
		}
		visited[curr] = struct{}{}
		if curr.VarName != "" && !(curr.PkgPath == key.ImportPath && curr.VarName == key.VarName) {
			imports[formatProviderSetName(curr.PkgPath, curr.VarName)] = struct{}{}
		}
		next = append(next, curr.Imports...)
	}

	// Depth-first search to build groups.
	var groups []outGroup
	inputVisited := new(typeutil.Map) // values are int, indices into groups or -1 for input.
	inputVisited.SetHasher(hash)
	var stk []types.Type
	for _, k := range set.Outputs() {
		// Start a DFS by picking a random unvisited node.
		if inputVisited.At(k) == nil {
			stk = append(stk, k)
		}

		// Run DFS
	dfs:
		for len(stk) > 0 {
			curr := stk[len(stk)-1]
			stk = stk[:len(stk)-1]
			if inputVisited.At(curr) != nil {
				continue
			}
			switch pv := set.For(curr); {
			case pv.IsNil():
				// This is an input.
				inputVisited.Set(curr, -1)
			case pv.IsArg():
				// This is an injector argument.
				inputVisited.Set(curr, -1)
			case pv.IsProvider():
				// Try to see if any args haven't been visited.
				p := pv.Provider()
				allPresent := true
				for _, arg := range p.Args {
					if inputVisited.At(arg.Type) == nil {
						allPresent = false
					}
				}
				if !allPresent {
					stk = append(stk, curr)
					for _, arg := range p.Args {
						if inputVisited.At(arg.Type) == nil {
							stk = append(stk, arg.Type)
						}
					}
					continue dfs
				}

				// Build up set of input types, match to a group.
				in := new(typeutil.Map)
				in.SetHasher(hash)
				for _, arg := range p.Args {
					i := inputVisited.At(arg.Type).(int)
					if i == -1 {
						in.Set(arg.Type, true)
					} else {
						mergeTypeSets(in, groups[i].inputs)
					}
				}
				for i := range groups {
					if sameTypeKeys(groups[i].inputs, in) {
						groups[i].outputs.Set(curr, p)
						inputVisited.Set(curr, i)
						continue dfs
					}
				}
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(curr, p)
				inputVisited.Set(curr, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			case pv.IsValue():
				v := pv.Value()
				for i := range groups {
					if groups[i].inputs.Len() == 0 {
						groups[i].outputs.Set(curr, v)
						inputVisited.Set(curr, i)
						continue dfs
					}
				}
				in := new(typeutil.Map)
				in.SetHasher(hash)
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(curr, v)
				inputVisited.Set(curr, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			case pv.IsField():
				// Try to see if the parent struct hasn't been visited.
				f := pv.Field()
				if inputVisited.At(f.Parent) == nil {
					stk = append(stk, curr, f.Parent)
					continue
				}
				// Build the input map for the parent struct.
				in := new(typeutil.Map)
				in.SetHasher(hash)
				i := inputVisited.At(f.Parent).(int)
				if i == -1 {
					in.Set(f.Parent, true)
				} else {
					mergeTypeSets(in, groups[i].inputs)
				}
				// Group all fields together under the same parent struct.
				for i := range groups {
					if sameTypeKeys(groups[i].inputs, in) {
						groups[i].outputs.Set(curr, f)
						inputVisited.Set(curr, i)
						continue dfs
					}
				}
				out := new(typeutil.Map)
				out.SetHasher(hash)
				out.Set(curr, f)
				inputVisited.Set(curr, len(groups))
				groups = append(groups, outGroup{
					inputs:  in,
					outputs: out,
				})
			default:
				panic("unreachable")
			}
		}
	}

	// Name and sort groups.
	for i := range groups {
		if groups[i].inputs.Len() == 0 {
			groups[i].name = "no inputs"
			continue
		}
		instr := make([]string, 0, groups[i].inputs.Len())
		groups[i].inputs.Iterate(func(k types.Type, _ interface{}) {
			instr = append(instr, types.TypeString(k, nil))
		})
		sort.Strings(instr)
		groups[i].name = strings.Join(instr, ", ")
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].inputs.Len() == groups[j].inputs.Len() {
			return groups[i].name < groups[j].name
		}
		return groups[i].inputs.Len() < groups[j].inputs.Len()
	})
	return groups, imports
}

func mergeTypeSets(dst, src *typeutil.Map) {
	src.Iterate(func(k types.Type, _ interface{}) {
		dst.Set(k, true)
	})
}

func sameTypeKeys(a, b *typeutil.Map) bool {
	if a.Len() != b.Len() {
		return false
	}
	same := true
	a.Iterate(func(k types.Type, _ interface{}) {
		if b.At(k) == nil {
			same = false
		}
	})
	return same
}

func sortSet(set interface{}) []string {
	rv := reflect.ValueOf(set)
	a := make([]string, 0, rv.Len())
	keys := rv.MapKeys()
	for _, k := range keys {
		a = append(a, k.String())
	}
	sort.Strings(a)
	return a
}

func formatProviderSetName(importPath, varName string) string {
	// Since varName is an identifier, it doesn't make sense to quote.
	return strconv.Quote(importPath) + "." + varName
}

func logErrors(errs []error) {
	for _, err := range errs {
		log.Println(strings.Replace(err.Error(), "\n", "\n\t", -1))
	}
}

type detailCmd struct {
	tags string
}

func (*detailCmd) Name() string { return "detail" }
func (*detailCmd) Synopsis() string {
	return "describe a single top-level provider set"
}
func (*detailCmd) Usage() string {
	return `detail [package] [name]

  detail is equivalent to show but only shows a provider set with the given name
  and does not describe injectors.
`
}
func (cmd *detailCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
}
func (cmd *detailCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return subcommands.ExitFailure
	}
	if len(f.Args()) != 2 {
		log.Println("detail requires two arguments: package and name")
		return subcommands.ExitFailure
	}
	pattern := []string{f.Args()[0]}
	name := f.Args()[1]
	info, errs := wire.Load(ctx, wd, os.Environ(), cmd.tags, pattern)
	if len(errs) > 0 {
		logErrors(errs)
		return subcommands.ExitFailure
	}
	var sb strings.Builder
	for k, set := range info.Sets {
		if set.VarName != name {
			continue
		}
		outGroups, imports := gather(info, k)
		sb.WriteString(k.String())
		for _, imp := range sortSet(imports) {
			sb.WriteString(fmt.Sprintf("\t%s\n", imp))
		}
		for i := range outGroups {
			sb.WriteString(fmt.Sprintf("\n\tOutputs given %s:\n", outGroups[i].name))
			out := make(map[string]token.Pos, outGroups[i].outputs.Len())
			outGroups[i].outputs.Iterate(func(t types.Type, v interface{}) {
				switch v := v.(type) {
				case *wire.Provider:
					out[types.TypeString(t, nil)] = v.Pos
				case *wire.Value:
					out[types.TypeString(t, nil)] = v.Pos
				case *wire.Field:
					out[types.TypeString(t, nil)] = v.Pos
				default:
					panic("unreachable")
				}
			})
			for _, t := range sortSet(out) {
				sb.WriteString(fmt.Sprintf("\t\t%s\n", t))
				sb.WriteString(fmt.Sprintf("\t\t\tat %v\n", info.Fset.Position(out[t])))
			}
		}
		// Print data to stdout as output
		fmt.Println(sb.String())
		return subcommands.ExitSuccess
	}
	return subcommands.ExitFailure
}

type graphCmd struct {
	tags    string
	browser bool
}

func (*graphCmd) Name() string { return "graph" }
func (*graphCmd) Synopsis() string {
	return "visualize providers as graph using grpahviz"
}
func (*graphCmd) Usage() string {
	return `graph [package] [name]

  Given a package and name, graph visualizes the dependencies of providers using Graphviz.
`
}
func (cmd *graphCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
	f.BoolVar(&cmd.browser, "browser", false, "show generated graph in browser")
}
func (cmd *graphCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	wd, err := os.Getwd()
	if err != nil {
		log.Println("failed to get working directory: ", err)
		return subcommands.ExitFailure
	}
	if len(f.Args()) != 2 {
		log.Println("graph requires two arguments: package and name")
		return subcommands.ExitFailure
	}
	pattern := []string{f.Args()[0]}
	name := f.Args()[1]
	gviz, errs := wire.Graph(ctx, wd, os.Environ(), pattern, name, cmd.tags)
	if len(errs) > 0 {
		logErrors(errs)
		log.Println("graph failed")
		return subcommands.ExitFailure
	}
	if cmd.browser {
		if err := showGraphInBrowser(gviz); err != nil {
			log.Println("failed to show graph in browser: ", err)
			return subcommands.ExitFailure
		} else {
			return subcommands.ExitSuccess
		}
	}
	// Print data to stdout as output
	fmt.Println(gviz.String())
	return subcommands.ExitSuccess
}

func showGraphInBrowser(gviz *wire.Graphviz) error {
	data := gviz.String()
	dot := strings.Replace(url.QueryEscape(data), "+", "%20", -1)
	// TODO: Make this customisable
	url := "https://edotor.net/#" + dot
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}

type lspCmd struct {
	tags string
}

func (*lspCmd) Name() string { return "lsp" }
func (*lspCmd) Synopsis() string {
	return "lsp starts interactive language server"
}
func (*lspCmd) Usage() string {
	return `lsp

  lsp starts an interactive language server that exchanges data in JSON.
`
}
func (cmd *lspCmd) SetFlags(f *flag.FlagSet) {
	f.StringVar(&cmd.tags, "tags", "", "append build tags to the default wirebuild")
}
func (cmd *lspCmd) Execute(ctx context.Context, f *flag.FlagSet, args ...interface{}) subcommands.ExitStatus {
	if len(f.Args()) != 0 {
		log.Println("lsp takes no arguments")
		return subcommands.ExitFailure
	}

	resCh := make(chan interface{})
	go func() {
		for {
			res := <-resCh
			lsp.SendMessage(res)
		}
	}()

	reader := bufio.NewReader(os.Stdin)
	for {
		buf, ok := lsp.ReadBuffer(reader)
		if !ok {
			lsp.SendError("failed to read buffer")
			continue
		}
		msg, ok := lsp.ParseMessage(buf)
		if !ok {
			lsp.SendError("failed to parse message")
			continue
		}
		method, ok := msg["method"]
		if !ok {
			lsp.SendError("message does not specify method")
			continue
		}
		if _, ok := msg["id"]; !ok {
			// Notification received
			// TODO: Sending as error for debugging purposes.
			lsp.SendError("received notification: %v\n", string(buf))
			switch method {
			case "initialized":
				// Ignore initialized notification.
			case "exit":
				return subcommands.ExitFailure
			// TODO: Support client with autosave disabled.
			case "textDocument/didOpen", "textDocument/didSave", "textDocument/didChange":
				notif := &lsp.DidSaveTextDocumentNotification{}
				if ok := lsp.ParseRequest(buf, notif); !ok {
					continue
				}
				go cmd.handlePublishDiagnosticsNotification(ctx, notif, resCh)
			default:
				lsp.SendError("invalid notification: %v\n", string(buf))
			}
		} else {
			// TODO: Sending as error for debugging purposes.
			lsp.SendError("received request: %v\n", string(buf))
			switch method {
			case "initialize":
				req := &lsp.InitializeRequest{}
				if ok := lsp.ParseRequest(buf, req); !ok {
					continue
				}
				go cmd.handleInitializeRequest(req, resCh)
			case "shutdown":
				req := &lsp.ShutdownRequest{}
				if ok := lsp.ParseRequest(buf, req); !ok {
					continue
				}
				go cmd.handleShutdownRequest(req, resCh)
			case "textDocument/codeLens":
				req := &lsp.CodeLensRequest{}
				if ok := lsp.ParseRequest(buf, req); !ok {
					continue
				}
				go cmd.handleCodeLensRequest(ctx, req, resCh)
			case "textDocument/definition":
				req := &lsp.DefinitionRequest{}
				if ok := lsp.ParseRequest(buf, req); !ok {
					continue
				}
				go cmd.handleDefinitionRequest(ctx, req, resCh)
			default:
				lsp.SendError("invalid method: %v\n", method)
			}
		}
	}
}

func (cmd *lspCmd) handleInitializeRequest(req *lsp.InitializeRequest, resCh chan interface{}) {
	res := &lsp.InitializeResponse{
		Jsonrpc: "2.0",
		Id:      req.Id,
		Result: &lsp.InitializeResult{
			Capabilities: lsp.ServerCapabilities{
				TextDocumentSync:   2, // 2: Incremental
				CodeLensProvider:   true,
				DefinitionProvider: true,
			},
		},
	}
	wsClientCap := req.Params.Capabilities.Workspace
	// configCap := wsClientCap.Configuration
	wsConfigCap := wsClientCap.WorkspaceFolders
	if wsConfigCap {
		wsServerCap := res.Result.Capabilities.Workspace
		wsServerCap.WorkspaceFolders.Supported = true
	}
	resCh <- res
}

func (cmd *lspCmd) handleShutdownRequest(req *lsp.ShutdownRequest, resCh chan interface{}) {
	res := &lsp.ShutdownResponse{
		Jsonrpc: "2.0",
		Id:      req.Id,
		Result:  nil,
	}
	resCh <- res
}

func (cmd *lspCmd) handleDefinitionRequest(ctx context.Context, req *lsp.DefinitionRequest, resCh chan interface{}) {
	res := &lsp.DefinitionResponse{
		Jsonrpc: "2.0",
		Id:      req.Id,
		Result:  nil,
	}
	url := lsp.ParseDocumentUri(req.Params.TextDocument.Uri)
	if url == nil {
		resCh <- res
		return
	}
	wd := filepath.Dir(url.Path)
	pattern := []string{"."}
	pkgs, errs := wire.LoadPackages(ctx, wd, os.Environ(), cmd.tags, pattern)
	if len(errs) > 0 {
		lsp.SendErrors(errs)
		resCh <- res
		return
	}
	if len(pkgs) != 1 {
		lsp.SendError("expected exactly one package")
		resCh <- res
		return
	}
	pkg := pkgs[0]
	line := req.Params.Position.Line
	char := req.Params.Position.Character
	pos := lsp.CalculatePos(pkg.Fset, url.Path, line, char)
	for _, f := range pkg.Syntax {
		file := pkg.Fset.File(f.Pos())
		if base := file.Base(); base <= int(pos) && int(pos) < base+file.Size() {
			path, ok := astutil.PathEnclosingInterval(f, pos, pos)
			if !ok {
				lsp.SendError("invalid position within file")
				resCh <- res
				return
			}
			node := path[0]
			if ident, ok := node.(*ast.Ident); ok {
				// TODO: Check this works for packages above the wd
				tarObj := pkg.TypesInfo.ObjectOf(ident)
				tarWd, ok := absolutePath(wd, tarObj.Pkg().Path())
				if !ok {
					lsp.SendError("unknown import path")
					resCh <- res
					return
				}
				tarPattern := []string{"."}
				tarPkgs, errs := wire.LoadPackages(ctx, tarWd, os.Environ(), cmd.tags, tarPattern)
				if len(errs) > 0 {
					lsp.SendErrors(errs)
					resCh <- res
					return
				}
				if len(tarPkgs) != 1 {
					lsp.SendError("expected exactly one package")
					resCh <- res
					return
				}
				tarPkg := tarPkgs[0]
				// TODO: Somehow jumps to a random position
				tarPosition := tarPkg.Fset.Position(tarObj.Pos())
				tarFilename := tarPosition.Filename
				tarLine := tarPosition.Line
				tarChar := tarPosition.Column
				res.Result = &lsp.Location{
					Uri: tarFilename,
					Range: lsp.Range{
						Start: lsp.Position{
							Line:      tarLine,
							Character: tarChar,
						},
						End: lsp.Position{
							Line:      tarLine,
							Character: tarChar,
						},
					},
				}
				resCh <- res
				return
			}
		}
	}
	resCh <- res
}

func absolutePath(wd string, importPath string) (string, bool) {
	tmp := "go list -f '{{.ImportPath}}:{{.Dir}}' all | grep "
	cmd := exec.Command("sh", "-c", tmp+importPath)
	cmd.Dir = wd
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		return "", false
	}
	line := string(stdout)
	parts := strings.Split(line, ":")
	absPath := strings.TrimSpace(parts[1])
	return absPath, true
}

func (cmd *lspCmd) handleCodeLensRequest(ctx context.Context, req *lsp.CodeLensRequest, resCh chan interface{}) {
	res := &lsp.CodeLensResponse{
		Jsonrpc: "2.0",
		Id:      req.Id,
		Result:  nil,
	}
	url := lsp.ParseDocumentUri(req.Params.TextDocument.Uri)
	if url == nil {
		resCh <- res
		return
	}
	wd := filepath.Dir(url.Path)
	pattern := []string{"."}
	info, errs := wire.Load(ctx, wd, os.Environ(), cmd.tags, pattern)
	if len(errs) > 0 {
		lsp.SendErrors(errs)
		resCh <- res
		return
	}
	if info == nil {
		resCh <- res
		return
	}
	var codeLenses []lsp.CodeLens
	for _, inj := range info.Injectors {
		file := info.Fset.File(inj.Pos)
		if file.Name() != url.Path {
			continue
		}
		codeLenses = append(codeLenses, makeCodeLens(
			info,
			inj.Pos,
			"Show Graph",
			"wireplus.showGraph",
			[]interface{}{wd, inj.FuncName}),
		)
	}
	for _, set := range info.Sets {
		file := info.Fset.File(set.Pos)
		if file.Name() != url.Path {
			continue
		}
		codeLenses = append(codeLenses, makeCodeLens(
			info,
			set.Pos,
			"Show Graph",
			"wireplus.showGraph",
			[]interface{}{wd, set.VarName}),
		)
		codeLenses = append(codeLenses, makeCodeLens(
			info,
			set.Pos,
			"Show Detail",
			"wireplus.showDetail",
			[]interface{}{wd, set.VarName}),
		)
	}
	res.Result = codeLenses
	resCh <- res
}

func makeCodeLens(info *wire.Info, pos token.Pos, title string, cmd string, args []interface{}) lsp.CodeLens {
	position := info.Fset.Position(pos)
	line := position.Line - 1
	char := position.Column - 1
	return lsp.CodeLens{
		Range: lsp.Range{
			Start: lsp.Position{
				Line:      line,
				Character: char,
			},
			End: lsp.Position{
				Line:      line,
				Character: char,
			},
		},
		Command: lsp.Command{
			Title:     title,
			Command:   cmd,
			Arguments: args,
		},
	}
}

func (cmd *lspCmd) handlePublishDiagnosticsNotification(ctx context.Context, event *lsp.DidSaveTextDocumentNotification, resCh chan interface{}) {
	url := lsp.ParseDocumentUri(event.Params.TextDocument.Uri)
	if url == nil {
		resCh <- nil
		return
	}
	wd := filepath.Dir(url.Path)
	pattern := []string{"."}
	_, errs := wire.Load(ctx, wd, os.Environ(), cmd.tags, pattern)
	// Need to return an empty slice when no error exists
	// to clear existing diagnostics
	diags := make([]lsp.Diagnostic, 0)
	for _, err := range errs {
		wireErr := err.(*wire.WireErr)
		position := wireErr.Position()
		if position.Filename != url.Path {
			continue
		}
		line := wireErr.Position().Line - 1
		char := wireErr.Position().Column - 1
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{
					Line:      line,
					Character: char,
				},
				End: lsp.Position{
					Line:      line + 1,
					Character: 0,
				},
			},
			Message: wireErr.Message(),
		})
	}
	res := &lsp.PublishDiagnosticsNotification{
		Jsonrpc: "2.0",
		Method:  "textDocument/publishDiagnostics",
		Params: lsp.PublishDiagnosticsParams{
			Uri:         event.Params.TextDocument.Uri,
			Diagnostics: diags,
		},
	}
	resCh <- res
}
