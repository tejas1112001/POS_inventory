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

// exemptTestDir holds integration-test helpers (build tag "integration",
// never compiled into a binary). They run DDL supplied by tests.
const exemptTestDir = "internal/testdb/"

func run(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	insp.WithStack([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
		if push {
			check(pass, n.(*ast.CallExpr), stack)
		}
		return true
	})
	return nil, nil
}

func check(pass *analysis.Pass, call *ast.CallExpr, stack []ast.Node) {
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
	if isPassThrough(pass, call.Args[idx], stack) {
		return
	}
	pass.Reportf(call.Args[idx].Pos(),
		"constsql: SQL passed to %s must be a constant; build identifiers only in %s", sel.Sel.Name, exemptFile)
}

// isPassThrough allows a wrapper method that is itself a query method (for
// example TenantTx.Exec(ctx, sql, args...)) to forward its own SQL parameter
// unchanged. Calls to the wrapper are checked like any other query method.
func isPassThrough(pass *analysis.Pass, arg ast.Expr, stack []ast.Node) bool {
	id, ok := arg.(*ast.Ident)
	if !ok {
		return false
	}
	var fd *ast.FuncDecl
	for i := len(stack) - 1; i >= 0; i-- {
		if f, ok := stack[i].(*ast.FuncDecl); ok {
			fd = f
			break
		}
	}
	if fd == nil || fd.Recv == nil || !queryMethods[fd.Name.Name] {
		return false
	}
	fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return false
	}
	i := sqlParamIndex(sig)
	if i < 0 {
		return false
	}
	return pass.TypesInfo.Uses[id] == sig.Params().At(i)
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
	// Test files never reach a binary and sometimes probe with dynamic SQL.
	return strings.HasSuffix(name, "/"+exemptFile) || strings.Contains(name, "/"+exemptTestDir) ||
		strings.HasSuffix(name, "_test.go")
}
