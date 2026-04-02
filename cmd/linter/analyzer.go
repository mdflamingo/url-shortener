package main

import (
	"go/ast"
	"go/types"

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
	info := pass.TypesInfo

	var currentFn *ast.FuncDecl

	inspect.Preorder([]ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.CallExpr)(nil),
	}, func(n ast.Node) {
		switch node := n.(type) {
		case *ast.FuncDecl:
			currentFn = node

		case *ast.CallExpr:
			checkCall(pass, node, info, currentFn)
		}
	})

	return nil, nil
}

func checkCall(pass *analysis.Pass, call *ast.CallExpr, info *types.Info, currentFn *ast.FuncDecl) {
	if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
		pass.Reportf(call.Pos(), "avoid using panic")
		return
	}

	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		pkgPath := getPackagePath(sel.X, info)

		switch pkgPath {
		case "log":
			if isFatalLogCall(sel.Sel.Name) && !isMainFunction(currentFn) {
				pass.Reportf(call.Pos(), "log.%s should only be used in main function", sel.Sel.Name)
				return
			}
		case "os":
			if sel.Sel.Name == "Exit" && !isMainFunction(currentFn) {
				pass.Reportf(call.Pos(), "os.Exit should only be used in main function")
				return
			}
		}
	}
}

func getPackagePath(node ast.Node, info *types.Info) string {
	ident, ok := node.(*ast.Ident)
	if !ok {
		return ""
	}

	obj, ok := info.Uses[ident]
	if !ok {
		return ""
	}

	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return ""
	}

	return pkgName.Imported().Path()
}

func isFatalLogCall(method string) bool {
	switch method {
	case "Fatal", "Fatalf", "Fatalln":
		return true
	}
	return false
}

func isMainFunction(fn *ast.FuncDecl) bool {
	if fn == nil {
		return false
	}
	return fn.Name.Name == "main" && fn.Recv == nil
}
