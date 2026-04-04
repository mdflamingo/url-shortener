// Package linter предоставляет анализатор кода для статической проверки Go программ.
// Анализатор запрещает использование panic, log.Fatal* и os.Exit за пределами функции main.
package main

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer реализует статический анализатор для обнаружения нежелательных вызовов.
// Проверяет следующие конструкции:
//   - panic: запрещен везде
//   - log.Fatal, log.Fatalf, log.Fatalln: разрешен только в функции main
//   - os.Exit: разрешен только в функции main
//
// Анализатор использует инспектор AST для обхода дерева разбора и проверки
// типов для корректного определения импортированных пакетов.
var Analyzer = &analysis.Analyzer{
	Name:     "linter",
	Doc:      "checks for panic calls and log.Fatal/os.Exit outside main function",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer}, // требуется для обхода AST
}

// run выполняет основную логику анализа.
// Параметры:
//   - pass: содержит информацию о проверяемом пакете (AST, типы, и т.д.)
//
// Возвращает:
//   - interface{}: всегда nil (анализатор не производит результатов)
//   - error: ошибка выполнения или nil при успехе
//
// Алгоритм:
//  1. Получает инспектор AST из зависимости
//  2. Обходит все функции и вызовы в пакете
//  3. Для каждого вызова проверяет соответствие правилам
func run(pass *analysis.Pass) (interface{}, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	info := pass.TypesInfo

	var currentFn *ast.FuncDecl

	// Предварительный обход AST с фильтрацией узлов
	inspect.Preorder([]ast.Node{
		(*ast.FuncDecl)(nil), // отслеживаем текущую функцию
		(*ast.CallExpr)(nil), // проверяем вызовы
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

// checkCall проверяет конкретный вызов функции на соответствие правилам.
//
// Параметры:
//   - pass: анализатор для создания отчетов
//   - call: AST узел вызова функции
//   - info: информация о типах для разрешения символов
//   - currentFn: текущая функция, в которой находится вызов
//
// Правила проверки:
//   - panic: всегда запрещен
//   - log.Fatal/Fatalf/Fatalln: разрешен только в main()
//   - os.Exit: разрешен только в main()
func checkCall(pass *analysis.Pass, call *ast.CallExpr, info *types.Info, currentFn *ast.FuncDecl) {
	// Проверка вызова panic()
	if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
		pass.Reportf(call.Pos(), "avoid using panic")
		return
	}

	// Проверка вызовов вида пакет.функция (например, log.Fatal)
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		pkgPath := getPackagePath(sel.X, info)

		switch pkgPath {
		case "log":
			// Проверка вызовов log.Fatal*
			if isFatalLogCall(sel.Sel.Name) && !isMainFunction(currentFn) {
				pass.Reportf(call.Pos(), "log.%s should only be used in main function", sel.Sel.Name)
				return
			}
		case "os":
			// Проверка вызова os.Exit
			if sel.Sel.Name == "Exit" && !isMainFunction(currentFn) {
				pass.Reportf(call.Pos(), "os.Exit should only be used in main function")
				return
			}
		}
	}
}

// getPackagePath извлекает путь импортированного пакета из идентификатора.
//
// Параметры:
//   - node: AST узел, представляющий пакет (обычно *ast.Ident)
//   - info: информация о типах для разрешения символов
//
// Возвращает:
//   - string: полный путь пакета (например, "log", "os") или пустую строку
//
// Пример:
//
//	getPackagePath(ast.Ident{Name: "log"}, info) // возвращает "log"
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

// isFatalLogCall проверяет, является ли вызов log.Fatal*.
//
// Параметры:
//   - method: имя метода (например, "Fatal", "Fatalf", "Fatalln")
//
// Возвращает:
//   - bool: true если метод относится к группе Fatal, иначе false
func isFatalLogCall(method string) bool {
	switch method {
	case "Fatal", "Fatalf", "Fatalln":
		return true
	}
	return false
}

// isMainFunction определяет, является ли функция main() (не метод).
//
// Параметры:
//   - fn: AST узел объявления функции
//
// Возвращает:
//   - bool: true если функция называется "main" и не имеет получателя
//
// Особенности:
//   - Проверяет только имя функции, не проверяет пакет
//   - Возвращает false для методов структур с именем main
func isMainFunction(fn *ast.FuncDecl) bool {
	if fn == nil {
		return false
	}
	return fn.Name.Name == "main" && fn.Recv == nil
}
