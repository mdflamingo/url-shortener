package main

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

var Analyzer = &analysis.Analyzer{
	Name:     "linter",
	Doc:      "checks for panic calls and log.Fatal/os.Exit outside main function",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

func run(pass *analysis.Pass) (interface{}, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	inspect.Preorder([]ast.Node{
		(*ast.CallExpr)(nil),
	}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if isPanicCall(pass, call) {
			pass.Reportf(call.Pos(), "avoid using panic")
			return
		}
	})

	inspect.Preorder([]ast.Node{
		(*ast.FuncDecl)(nil),
	}, func(n ast.Node) {
		fn := n.(*ast.FuncDecl)
		if !isMainFunction(fn) {
			inspectFnForFatalCalls(pass, fn, inspect)
		}
	})

	return nil, nil
}

func isPanicCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "log" {
			if sel.Sel.Name == "Fatal" || sel.Sel.Name == "Fatalf" || sel.Sel.Name == "Fatalln" {
				return true
			}
		}
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "os" && sel.Sel.Name == "Exit" {
			return true
		}
	}

	if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
		return true
	}

	return false
}

func isMainFunction(fn *ast.FuncDecl) bool {
	return fn.Name.Name == "main" && fn.Recv == nil
}

func inspectFnForFatalCalls(pass *analysis.Pass, fn *ast.FuncDecl, inspect *inspector.Inspector) {
	_ = []ast.Node{
		(*ast.CallExpr)(nil),
	}
	inspect.Preorder([]ast.Node{fn.Body}, func(n ast.Node) {
		if call, ok := n.(*ast.CallExpr); ok {
			if isPanicCall(pass, call) {
				pass.Reportf(call.Pos(), "log.Fatal, log.Fatalf, log.Fatalln or os.Exit should only be used in main function")
			}
		}
	})
}
