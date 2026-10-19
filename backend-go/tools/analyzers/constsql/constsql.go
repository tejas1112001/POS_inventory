// Package constsql reports database calls whose SQL text is not a
// compile-time constant. Only internal/db/ident.go may build SQL from
// validated identifiers (TENANCY §4).
//
// A call is checked when the method name is one of the known query methods
// and the parameter after a context.Context (or the first parameter) is a
// string. That covers pgx, pgxpool, database/sql and sqlc's DBTX interface.
package constsql

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer is the constsql analyzer.
var Analyzer = &analysis.Analyzer{
	Name:     "constsql",
	Doc:      "SQL passed to database calls must be a compile-time constant",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

var queryMethods = map[string]bool{
	"Exec": true, "Query": true, "QueryRow": true,
	"ExecContext": true, "QueryContext": true, "QueryRowContext": true,
	"Prepare": true, "PrepareContext": true, "Queue": true,
}

// exemptFile is the one file allowed to build SQL from quoted identifiers.
const exemptFile = "internal/db/ident.go"

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !queryMethods[sel.Sel.Name] {
			return
		}
		fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
		if !ok {
			return
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() == nil {
			return
		}
		idx := sqlParamIndex(sig)
		if idx < 0 || idx >= len(call.Args) {
			return
		}
		if isExempt(pass, call) {
			return
		}
		tv, ok := pass.TypesInfo.Types[call.Args[idx]]
		if ok && tv.Value != nil {
			return // constant
		}
		pass.Reportf(call.Args[idx].Pos(),
			"constsql: SQL passed to %s must be a constant; build identifiers only in %s", sel.Sel.Name, exemptFile)
	})
	return nil, nil
}

// sqlParamIndex returns the index of the SQL string parameter, or -1.
func sqlParamIndex(sig *types.Signature) int {
	params := sig.Params()
	if params.Len() == 0 {
		return -1
	}
	i := 0
	if isContext(params.At(0).Type()) {
		i = 1
	}
	if i >= params.Len() {
		return -1
	}
	if b, ok := params.At(i).Type().Underlying().(*types.Basic); ok && b.Kind() == types.String {
		return i
	}
	return -1
}

func isContext(t types.Type) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "context" && obj.Name() == "Context"
}

func isExempt(pass *analysis.Pass, call *ast.CallExpr) bool {
	name := filepath.ToSlash(pass.Fset.Position(call.Pos()).Filename)
	return strings.HasSuffix(name, "/"+exemptFile)
}
