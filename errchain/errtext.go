package errchain

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/code"
)

var Analyzer = &analysis.Analyzer{
	Name:     "errchain",
	Doc:      "Checks that error chains contain information about place where problem occurred.",
	Run:      run,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
}

const diagnosticMessage = "Error message must point to the place where it had happened"
const helpURL = "https://bit.ly/err-chains"

// go run -ldflags "-X github.com/iimos/go-check-err-chains/errchain.debug=1" .
var debug = ""

func isDebug() bool {
	return debug != ""
}

func run(pass *analysis.Pass) (interface{}, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{(*ast.File)(nil)}

	if code.IsMainLike(pass) {
		return nil, nil
	}

	insp.Preorder(nodeFilter, func(node ast.Node) {
		if file, ok := node.(*ast.File); ok {
			if isGenerated(file) || isTest(pass, file) {
				return
			}
			for _, decl := range file.Decls {
				if funcDecl, ok := decl.(*ast.FuncDecl); ok {
					handleFuncDecl(pass, funcDecl)
				}
			}
		}
	})

	return nil, nil
}

func handleFuncDecl(pass *analysis.Pass, funcDecl *ast.FuncDecl) {
	if funcDecl.Name == nil || funcDecl.Body == nil {
		return
	}

	if !ast.IsExported(funcDecl.Name.Name) || !isReturnsError(funcDecl) {
		return
	}

	ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
		handleFuncBody(pass, funcDecl, node)
		return true
	})
}

// errorPrefixes returns a set of possible prefixes a given function's error message can start with.
func errorPrefixes(pkg *types.Package, fn *ast.FuncDecl) []string {
	if fn.Name == nil {
		return nil
	}

	prefixes := make([]string, 0, 3)
	prefixes = append(prefixes, pkg.Name()+": ")

	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		funcPref := pkg.Name() + "." + fn.Name.Name + ": "
		return append(prefixes, funcPref)
	}

	recvType := fn.Recv.List[0].Type

	switch recv := recvType.(type) {
	case *ast.StarExpr:
		if ident, ok := recv.X.(*ast.Ident); ok {
			pref1 := fmt.Sprintf("%s.%s.%s: ", pkg.Name(), ident.Name, fn.Name.Name)
			pref2 := fmt.Sprintf("%s.(*%s).%s: ", pkg.Name(), ident.Name, fn.Name.Name)
			pref3 := fmt.Sprintf("%s.%s: ", pkg.Name(), ident.Name)
			prefixes = append(prefixes, pref1, pref2, pref3)
		} else {
			return nil
		}
	case *ast.Ident:
		pref1 := fmt.Sprintf("%s.%s.%s: ", pkg.Name(), recv.Name, fn.Name.Name)
		pref2 := fmt.Sprintf("%s.%s: ", pkg.Name(), recv.Name)
		prefixes = append(prefixes, pref1, pref2)
	default:
		return nil
	}
	return prefixes
}

// isReturnsError tells whether an ast.FuncDecl returns an error as a last result.
func isReturnsError(funcDecl *ast.FuncDecl) bool {
	if funcDecl.Type == nil || funcDecl.Type.Results == nil {
		return false
	}

	list := funcDecl.Type.Results.List
	if len(list) == 0 {
		return false
	}

	lastType := list[len(list)-1].Type
	if ident, ok := lastType.(*ast.Ident); ok {
		if ident.Name == "error" {
			return true
		}
	}
	return false
}

func handleFuncBody(pass *analysis.Pass, parentFunc *ast.FuncDecl, node ast.Node) {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return
	}

	if len(call.Args) == 0 {
		return
	}

	callName := code.CallName(pass, call)
	switch callName {
	case "errors.New", "fmt.Errorf":
		format, ok := constantValueString(pass, call.Args[0])
		if !ok {
			return
		}

		formatArgs := make([]interface{}, 0, len(call.Args)-1)
		for i := 1; i < len(call.Args); i++ {
			formatArgs = append(formatArgs, printableExpr{
				pass: pass,
				expr: call.Args[i],
			})
		}

		errorMessage := fmt.Sprintf(format, formatArgs...)
		prefix, err := parsePrefix(errorMessage)

		report := func(err *prefixError) {
			if isDebug() {
				fmt.Printf("[DEBUG] errchain: %s(%q); err=%+v\n", callName, errorMessage, err)
			}
			var msg string
			switch err.errType {
			case errNoPrefix:
				recoms := generatePrefixRecomendations(pass, parentFunc)
				msg = diagnosticMessage + ": " + recoms
			default:
				msg = diagnosticMessage + ": " + err.errType.Error()
			}
			pass.Reportf(node.Pos(), msg)
		}

		if err != nil {
			switch err {
			case errNoPrefix:
				report(&prefixError{errType: errNoPrefix})
				return
			case errInvalidSyntax:
				if prefix.match(pass.Pkg, parentFunc) == nil {
					report(&prefixError{errType: errInvalidSyntax})
					// todo: report("seems like correct prefix but syntax is wrong")
					return
				}
				report(&prefixError{errType: errNoPrefix})
				return
			default:
				if isDebug() {
					panic("unexpected error type: " + err.Error())
				}
			}
		}

		if err := prefix.match(pass.Pkg, parentFunc); err != nil {
			report(err)
		}
	}
}

func generatePrefixRecomendations(pass *analysis.Pass, parentFunc *ast.FuncDecl) string {
	buf := strings.Builder{}
	buf.WriteString("Consider starting message with one of the following strings: ")
	for i, prefix := range errorPrefixes(pass.Pkg, parentFunc) {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(strconv.Quote(prefix))
	}
	return buf.String()
}

type location struct {
	pkg       string
	recv      string
	fn        string
	isRecvPtr bool
}

type errorKind string

func (k errorKind) Error() string {
	return string(k)
}

var (
	errNoPrefix         = errorKind("no prefix found")
	errPackageMismatch  = errorKind("package name mismatch")
	errPrefixDuplicate  = errorKind("prefix duplicate")
	errInvalidSyntax    = errorKind("syntax is wrong")
	errFuncNotFound     = errorKind("neither func nor struct has been found")
	errMethodNotFound   = errorKind("method not found")
	errRecieverNotFound = errorKind("reciever not found")
	errNoPointer        = errorKind("reciever has no pointer")
)

type prefixError struct {
	errType      errorKind
	got          string
	expect       string
	parsedPrefix location
}

func (loc location) match(pkg *types.Package, fn *ast.FuncDecl) *prefixError {
	if loc.pkg == "" {
		return &prefixError{errType: errNoPrefix, got: loc.pkg, expect: pkg.Name(), parsedPrefix: loc}
	}

	if !strings.HasSuffix(pkg.Path(), loc.pkg) {
		return &prefixError{errType: errPackageMismatch, got: loc.pkg, expect: pkg.Name(), parsedPrefix: loc}
	}

	recieverName, isRecieverPointer := recvString(fn)
	functionName := fn.Name.Name

	// pkg only
	if loc.recv == "" && loc.fn == "" {
		return nil
	}

	// pkg.Func, pkg.Struct, pkg.Method
	if loc.recv == "" && loc.fn != "" {
		if loc.fn == recieverName {
			// pkg.Struct
			return nil
		}
		if loc.fn == functionName {
			// pkg.Func, pkg.Method
			return nil
		}
		return &prefixError{
			errType:      errFuncNotFound,
			got:          loc.fn,
			expect:       functionName + " or " + recieverName,
			parsedPrefix: loc,
		}
	}

	// pkg.Struct.Method, pkg.(*Struct).Method
	if loc.recv != "" && loc.fn != "" {
		if loc.recv == recieverName && loc.fn != functionName {
			// err: method not found
			return &prefixError{
				errType:      errMethodNotFound,
				got:          loc.fn,
				expect:       functionName,
				parsedPrefix: loc,
			}
		}
		if loc.recv != recieverName && loc.fn == functionName {
			// err: reciever not found
			return &prefixError{
				errType:      errRecieverNotFound,
				got:          loc.recv,
				expect:       recieverName,
				parsedPrefix: loc,
			}
		}
		if loc.recv != recieverName && loc.fn != functionName {
			// err: method not found
			return &prefixError{
				errType:      errMethodNotFound,
				got:          loc.recv + "." + loc.fn,
				expect:       recieverName + "." + functionName,
				parsedPrefix: loc,
			}
		}
		if loc.recv == recieverName && loc.fn == functionName {
			if loc.isRecvPtr && !isRecieverPointer {
				// err: reciever has no pointer
				return &prefixError{
					errType:      errNoPointer,
					got:          "(*" + loc.recv + ")",
					expect:       recieverName,
					parsedPrefix: loc,
				}
			}
			return nil
		}
	}

	// unrechable
	if isDebug() {
		panic("imposible")
	}
	return nil
}

// recvString returns a string representation of the functions reciever.
func recvString(fn *ast.FuncDecl) (recieverName string, isPointer bool) {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "", false
	}

	recvType := fn.Recv.List[0].Type

	if star, ok := recvType.(*ast.StarExpr); ok {
		isPointer = true
		recvType = star.X
	}

	if r, ok := recvType.(*ast.Ident); ok {
		return r.Name, isPointer
	}
	return "", false
}

func parsePrefix(errorMessage string) (loc location, err error) {
	const sep = ": "
	i := strings.Index(errorMessage, sep)
	if i < 0 {
		return loc, errNoPrefix
	}

	split := strings.SplitN(errorMessage[:i], ".", 4)
	switch len(split) {
	case 1:
		loc.pkg = split[0]
	case 2:
		loc.pkg = split[0]
		loc.fn = split[1]
	case 3:
		loc.pkg = split[0]
		loc.recv = split[1]
		loc.fn = split[2]
	default:
		loc.pkg = split[0]
		loc.recv = split[1]
		loc.fn = split[2]
		return loc, errInvalidSyntax
	}

	if loc.recv != "" && !token.IsIdentifier(loc.recv) {
		return loc, errInvalidSyntax
	}
	if loc.fn != "" && !token.IsIdentifier(loc.fn) {
		return loc, errInvalidSyntax
	}

	if strings.HasPrefix(loc.recv, "(*") {
		loc.recv = loc.recv[2:]
		if strings.HasSuffix(loc.recv, ")") {
			loc.recv = loc.recv[:len(loc.recv)-1]
			loc.isRecvPtr = true
		} else {
			return loc, errInvalidSyntax
		}
	}
	return loc, nil
}

func constantValue(pass *analysis.Pass, expr ast.Expr) (interface{}, bool) {
	val := pass.TypesInfo.Types[expr].Value
	if val == nil {
		return nil, false
	}
	return constant.Val(val), true
}

func constantValueString(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	val, ok := constantValue(pass, expr)
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// An exprString gives a text representation of an ast.Expr which are concise and easy to read.
func exprString(expr ast.Expr, depth int) string {
	if depth > 2 {
		return "?"
	}

	switch x := expr.(type) {

	case *ast.BasicLit:
		return x.Value

	case *ast.Ident:
		return x.Name

	case *ast.SelectorExpr:
		a := exprString(x.X, depth+1)
		b := exprString(x.Sel, depth+1)
		if a == "?" || b == "?" {
			return "?"
		}
		return a + "." + b

	case *ast.BinaryExpr:
		return exprString(x.X, depth+1) + x.Op.String() + exprString(x.Y, depth+1)

	case *ast.UnaryExpr:
		ex := exprString(x.X, depth+1)
		if ex == "?" {
			return "?"
		}
		return x.Op.String() + ex

	case *ast.CallExpr:
		fn := exprString(x.Fun, depth+1)
		if fn == "?" {
			return "?"
		}
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = exprString(a, depth+1)
		}
		return fn + "(" + strings.Join(args, ", ") + ")"

	case *ast.IndexExpr:
		return exprString(x.X, depth+1) + "[?]"

	case *ast.SliceExpr:
		return exprString(x.X, depth+1) + "[?]"

	default:
		return "?"
	}
}

// An isGenerated tells whether a file is automatically generated.
func isGenerated(file *ast.File) bool {
	for _, commentGroup := range file.Comments {
		for _, c := range commentGroup.List {
			if strings.Contains(c.Text, "DO NOT EDIT") {
				return true
			}
			text := strings.ToLower(c.Text)
			if strings.Contains(text, " generated by ") {
				return true
			}
		}
	}
	return false
}

// An isTest tells whether a given file is a test file.
func isTest(pass *analysis.Pass, file *ast.File) bool {
	f := pass.Fset.File(file.Pos())
	if f == nil {
		return false
	}
	return strings.HasSuffix(f.Name(), "_test.go")
}

// A printableExpr wraps ast.Expr and make it printable via fmt.Errorf function. It implements fmt.Formatter.
// Main reason for this is to print any ast.Expr via fmt.Errorf via any format string regardless of used verbs.
// For a constant it prints a constant's value, for variables it prints a variable's name, and for other expressions
// it is trying to print a short readable description.
type printableExpr struct {
	pass *analysis.Pass
	expr ast.Expr
}

var _ fmt.Formatter = printableExpr{}

// Format implements fmt.Formatter.
func (e printableExpr) Format(s fmt.State, verb rune) {
	v, ok := constantValue(e.pass, e.expr)
	if !ok {
		_, _ = fmt.Fprintf(s, "{%s}", exprString(e.expr, 0))
		return
	}
	_, _ = fmt.Fprintf(s, "%v", v)
}
