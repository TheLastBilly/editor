package godebug

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"

	"github.com/davecgh/go-spew/spew"
)

type Annotator struct {
	fset              *token.FileSet
	debugPkgName      string
	debugVarPrefix    string
	debugVarNameIndex int
	fileIndex         int

	debugIndex         int
	builtDebugLineStmt bool
}

func NewAnnotator() *Annotator {
	ann := &Annotator{}
	ann.debugPkgName = string('Σ')
	ann.debugVarPrefix = string('Σ')
	return ann
}

func (ann *Annotator) ParseAnnotateFile(filename string, src interface{}) (*ast.File, error) {
	mode := parser.ParseComments // to support cgo directives on imports
	astFile, err := parser.ParseFile(ann.fset, filename, src, mode)
	if err != nil {
		return nil, err
	}

	// don't annotate these packages
	switch astFile.Name.Name {
	case "godebugconfig", "debug":
		return astFile, nil
	}

	ann.annotate(astFile)

	return astFile, nil
}

func (ann *Annotator) PrintSimple(w io.Writer, astFile *ast.File) error {
	cfg := &printer.Config{Mode: printer.RawFormat}
	return cfg.Fprint(w, ann.fset, astFile)
}

//----------

func (ann *Annotator) annotate(node ast.Node) {
	ctx := &Ctx{}
	switch t := node.(type) {
	case *ast.File:
		ann.visitFile(ctx, t)
	default:
		spew.Dump("node", t)
	}
}

//----------

func (ann *Annotator) visitFile(ctx *Ctx, file *ast.File) {
	for _, d := range file.Decls {
		ann.visitDeclFromFile(ctx, d)
	}
}

func (ann *Annotator) visitDeclFromFile(ctx *Ctx, decl ast.Decl) {
	switch t := decl.(type) {
	case *ast.FuncDecl:
		ann.visitFuncDecl(ctx, t)
	case *ast.GenDecl:
		// do nothing
	default:
		spew.Dump("decl", t)
	}
}

func (ann *Annotator) visitFuncDecl(ctx *Ctx, fd *ast.FuncDecl) {
	// don't annotate String functions to avoid endless loops recursion
	if fd.Name.Name == "String" {
		return
	}

	// create new blockstmt to contain args debug stmts
	pos := fd.Type.End()
	bs := &ast.BlockStmt{List: []ast.Stmt{}}

	hasParams := len(fd.Type.Params.List) > 0
	if hasParams {
		// visit parameters
		ctx3, _ := ctx.withStmtIter(&bs.List)
		exprs := ann.visitFieldList(ctx3, fd.Type.Params)

		// debug line
		ce := ann.newDebugCallExpr("IL", exprs...)
		stmt2 := ann.newDebugLineStmt(ctx3, pos, ce)
		ctx3.insertInStmtList(stmt2)
	}

	// visit body
	ctx2 := ctx.withFuncType(fd.Type)
	if fd.Body != nil {
		ann.visitBlockStmt(ctx2, fd.Body)
	}

	if hasParams {
		// insert blockstmt at the top of the body
		ctx4, _ := ctx.withStmtIter(&fd.Body.List)
		ctx4.insertInStmtListBefore(bs) // index 0
	}
}

//----------

func (ann *Annotator) visitBlockStmt(ctx *Ctx, bs *ast.BlockStmt) {
	ann.visitStmtList(ctx, &bs.List)
}

func (ann *Annotator) visitExprStmt(ctx *Ctx, es *ast.ExprStmt) {
	pos := es.End() // replacements could make position 0
	e := ann.visitExpr(ctx, &es.X)
	stmt := ann.newDebugLineStmt(ctx, pos, e)
	ctx.insertInStmtList(stmt)
}

func (ann *Annotator) visitAssignStmt(ctx *Ctx, as *ast.AssignStmt) {
	pos := as.End() // replacements could make position 0

	ctx2 := ctx.withInsertStmtAfter(false)

	ctx3 := ctx2
	if len(as.Rhs) >= 2 {
		// ex: a[i], a[j] = a[j], a[i] // a[j] returns 1 result
		ctx3 = ctx3.withNResults(1)
	} else {
		ctx3 = ctx3.withNResults(len(as.Lhs))
	}
	ctx3 = ctx3.withResultInVar()
	rhs := ann.visitExprList(ctx3, &as.Rhs)

	if ctx.assignStmtIgnoreLhs() {
		ce1 := ann.newDebugCallExpr("IL", rhs...)
		stmt2 := ann.newDebugLineStmt(ctx, pos, ce1)
		ctx.insertInStmtList(stmt2)
		return
	}

	rhsId := ann.newDebugCallExpr("IL", rhs...)

	ctx4 := ctx.withInsertStmtAfter(true)
	ctx4 = ctx4.withNResults(1) // a[i] // a returns 1 result, not zero
	lhs := ann.visitExprList(ctx4, &as.Lhs)

	lhsId := ann.newDebugCallExpr("IL", lhs...)

	ce3 := ann.newDebugCallExpr("IA", lhsId, rhsId)
	stmt2 := ann.newDebugLineStmt(ctx4, pos, ce3)
	ctx4.insertInStmtList(stmt2)
}

func (ann *Annotator) visitTypeSwitchStmt(ctx *Ctx, tss *ast.TypeSwitchStmt) {
	// TODO: init stmt
	//ann.visitStmt(ctx, tss.Init)

	ctx2 := ctx.withAssignStmtIgnoreLhs()
	ann.visitStmt(ctx2, tss.Assign)

	ann.visitBlockStmt(ctx, tss.Body)
}

func (ann *Annotator) visitSwitchStmt(ctx *Ctx, ss *ast.SwitchStmt) {
	if ss.Init != nil {
		bs := ann.wrapInBlockStmt(ctx, ss)
		bs.List = append([]ast.Stmt{ss.Init}, bs.List...)
		ss.Init = nil
		ann.visitBlockStmt(ctx, bs)
		return
	}

	if ss.Tag != nil {
		pos := ss.Tag.End() // replacements could make position 0
		ctx2 := ctx.withResultInVar()
		e := ann.visitExpr(ctx2, &ss.Tag)
		stmt2 := ann.newDebugLineStmt(ctx, pos, e)
		ctx.insertInStmtListBefore(stmt2)
	}

	ann.visitBlockStmt(ctx, ss.Body)
}

func (ann *Annotator) visitIfStmt(ctx *Ctx, is *ast.IfStmt) {
	if is.Init != nil {
		// wrap in block stmt to have init variables valid only in block
		bs := ann.wrapInBlockStmt(ctx, is)
		bs.List = append([]ast.Stmt{is.Init}, bs.List...)
		is.Init = nil
		ann.visitBlockStmt(ctx, bs)
		return
	}

	// condition
	pos := is.Cond.End() // replacements could make position 0
	ctx2 := ctx.withNResults(1).withResultInVar()
	e := ann.visitExpr(ctx2, &is.Cond)
	stmt2 := ann.newDebugLineStmt(ctx, pos, e)
	ctx.insertInStmtListBefore(stmt2)

	ann.visitBlockStmt(ctx, is.Body)

	if is.Else != nil {
		switch t := is.Else.(type) {
		case *ast.IfStmt:
			// "else if"
			is2 := t
			// wrap in block stmt to have init variables valid only in block
			bs2 := &ast.BlockStmt{List: []ast.Stmt{is2}}
			is.Else = bs2 // replace
			if is2.Init != nil {
				bs2.List = append([]ast.Stmt{is2.Init}, bs2.List...)
				is2.Init = nil
			}
			ann.visitBlockStmt(ctx, bs2)
			return
		case *ast.BlockStmt:
			// else
			ann.visitBlockStmt(ctx, t)
		default:
			spew.Dump("todo: visitIfStmt: ", t)
		}
	}
}

func (ann *Annotator) visitForStmt(ctx *Ctx, fs *ast.ForStmt) {
	if fs.Cond != nil {
		pos := fs.Cond.End()

		// create ifstmt to break the loop
		ue := &ast.UnaryExpr{Op: token.NOT, X: fs.Cond} // negate
		is := &ast.IfStmt{If: fs.Pos(), Cond: ue, Body: &ast.BlockStmt{}}
		fs.Cond = nil // clear forstmt condition

		// insert break inside ifstmt
		brk := &ast.BranchStmt{Tok: token.BREAK}
		is.Body.List = append(is.Body.List, brk)

		// blockstmt to contain the code to be inserted
		bs := &ast.BlockStmt{List: []ast.Stmt{is}}

		// visit condition
		ctx3, _ := ctx.withStmtIter(&bs.List) // index at 0
		e := ann.visitExpr(ctx3, &ue.X)

		// ifstmt condition debug line (create debug line before visiting body)
		stmt2 := ann.newDebugLineStmt(ctx3, pos, e)
		ctx3.insertInStmtListBefore(stmt2)

		// visit body (creates bigger debug line indexes)
		ann.visitBlockStmt(ctx, fs.Body)

		// insert created blockstmt at the top (after visiting body).
		ctx4, _ := ctx.withStmtIter(&fs.Body.List) // index at 0
		ctx4.insertInStmtListBefore(bs)

		return
	}

	ann.visitBlockStmt(ctx, fs.Body)
}

func (ann *Annotator) visitRangeStmt(ctx *Ctx, rs *ast.RangeStmt) {
	pos := rs.X.End()

	ctx2 := ctx.withNResults(1)
	x := ann.visitExpr(ctx2, &rs.X)

	// TODO: context to discard X when visiting rs.X above?
	// assign x to anon (not using X value)
	as2 := ann.newAssignStmt11(anonIdent(), x)
	as2.Tok = token.ASSIGN
	ctx.insertInStmtList(as2)

	// length of x
	ce5 := &ast.CallExpr{Fun: ast.NewIdent("len"), Args: []ast.Expr{rs.X}}
	ce6 := ann.newDebugCallExpr("IVl", ce5)
	lenId := ann.assignToNewIdent(ctx, ce6)

	// key and value
	lhs := []ast.Expr{}
	if rs.Key != nil {
		ce := ann.newDebugCallExpr("IV", rs.Key)
		if isAnonIdent(rs.Key) {
			ce = ann.newDebugCallExpr("IAn")
		}
		lhs = append(lhs, ce)
	}
	if rs.Value != nil {
		ce := ann.newDebugCallExpr("IV", rs.Value)
		if isAnonIdent(rs.Value) {
			ce = ann.newDebugCallExpr("IAn")
		}
		lhs = append(lhs, ce)
	}

	// blockstmt to contain the code to be inserted
	bs := &ast.BlockStmt{}
	ctx3, _ := ctx.withStmtIter(&bs.List) // index at 0

	rhs := []ast.Expr{lenId}
	ce1 := ann.newDebugCallExpr("IL", rhs...)
	rhsId := ann.assignToNewIdent(ctx3, ce1)

	ce2 := ann.newDebugCallExpr("IL", lhs...)
	lhsId := ann.assignToNewIdent(ctx3, ce2)

	as1 := ann.newDebugCallExpr("IA", lhsId, rhsId)

	// create debug line before visiting range body
	stmt2 := ann.newDebugLineStmt(ctx3, pos, as1)
	ctx3.insertInStmtListBefore(stmt2)

	// visit range body
	ann.visitBlockStmt(ctx, rs.Body)

	// insert created blockstmt at the top (after visiting body).
	ctx4, _ := ctx.withStmtIter(&rs.Body.List) // index at 0
	ctx4.insertInStmtListBefore(bs)
}

func (ann *Annotator) visitLabeledStmt(ctx *Ctx, ls *ast.LabeledStmt) {
	if ls.Stmt == nil {
		return
	}

	// handle non-empty stmts
	if _, ok := ls.Stmt.(*ast.EmptyStmt); !ok {
		// create blockstmt to keep the visit stmts
		bs := &ast.BlockStmt{List: []ast.Stmt{ls.Stmt}}
		ctx3, _ := ctx.withStmtIter(&bs.List) // index at 0
		ann.visitStmt(ctx3, ls.Stmt)

		// assign empty stmt
		ls.Stmt = &ast.EmptyStmt{}

		// insert created stmts
		for _, s := range bs.List {
			ctx.insertInStmtListAfter(s)
		}
	}
}

func (ann *Annotator) visitReturnStmt(ctx *Ctx, rs *ast.ReturnStmt) {
	ft, ok := ctx.funcType()
	if !ok {
		return
	}

	// functype number of results to return
	ftNResults := ft.Results.NumFields()
	if ftNResults == 0 {
		return
	}

	pos := rs.End()

	// naked return, use results ids
	if len(rs.Results) == 0 {
		var w []ast.Expr
		for _, f := range ft.Results.List {
			for _, id := range f.Names {
				w = append(w, id)
			}
		}
		rs.Results = w
	}

	// visit results
	n := ftNResults
	if len(rs.Results) > 1 { // ex: return 1, f(1), 1
		n = 1
	}
	ctx2 := ctx.withNResults(n).withResultInVar()
	exprs := ann.visitExprList(ctx2, &rs.Results)

	ce := ann.newDebugCallExpr("IL", exprs...)
	stmt2 := ann.newDebugLineStmt(ctx, pos, ce)
	ctx.insertInStmtListBefore(stmt2)
}

func (ann *Annotator) visitDeferStmt(ctx *Ctx, ds *ast.DeferStmt) {
	ann.visitDeferCallStmt(ctx, &ds.Call)
}
func (ann *Annotator) visitDeferCallStmt(ctx *Ctx, cep **ast.CallExpr) {
	// assign arguments to tmp variables
	ce := *cep
	if len(ce.Args) > 0 {
		args2 := make([]ast.Expr, len(ce.Args))
		copy(args2, ce.Args)
		ids := ann.assignToNewIdents(ctx, len(ce.Args), args2...)
		for i := range ce.Args {
			ce.Args[i] = ids[i]
		}
	}

	// replace func call with wrapped function
	bs := &ast.BlockStmt{List: []ast.Stmt{
		&ast.ExprStmt{X: ce},
	}}
	*cep = &ast.CallExpr{
		Fun: &ast.FuncLit{
			Type: &ast.FuncType{Params: &ast.FieldList{}},
			Body: bs,
		},
	}

	ann.visitBlockStmt(ctx, bs)
}

func (ann *Annotator) visitDeclStmt(ctx *Ctx, ds *ast.DeclStmt) {
	// decl: const, type, var

	if gd, ok := ds.Decl.(*ast.GenDecl); ok {
		// gendecl: const, type, var, import

		for _, s := range gd.Specs {
			ann.visitSpec(ctx, s)
		}
	}
}

func (ann *Annotator) visitBranchStmt(ctx *Ctx, bs *ast.BranchStmt) {
	pos := bs.Pos()
	ce := ann.newDebugCallExpr("IBr")
	stmt := ann.newDebugLineStmt(ctx, pos, ce)
	ctx.insertInStmtList(stmt)
}

func (ann *Annotator) visitIncDecStmt(ctx *Ctx, ids *ast.IncDecStmt) {
	pos := ids.End()

	e1 := ann.visitExpr(ctx, &ids.X)

	ctx2 := ctx.withInsertStmtAfter(true)
	e2 := ann.visitExpr(ctx2, &ids.X)

	l1 := ann.newDebugCallExpr("IL", e1)
	l2 := ann.newDebugCallExpr("IL", e2)

	ce3 := ann.newDebugCallExpr("IA", l2, l1)
	stmt := ann.newDebugLineStmt(ctx, pos, ce3)
	ctx2.insertInStmtList(stmt)
}

func (ann *Annotator) visitSendStmt(ctx *Ctx, ss *ast.SendStmt) {
	pos := ss.End()

	ctx2 := ctx.withNResults(1).withResultInVar()
	val := ann.visitExpr(ctx2, &ss.Value)

	ctx3 := ctx.withInsertStmtAfter(true)

	ch := ann.visitExpr(ctx3, &ss.Chan)

	ce := ann.newDebugCallExpr("IS", ch, val)
	stmt := ann.newDebugLineStmt(ctx3, pos, ce)
	ctx3.insertInStmtList(stmt)
}

func (ann *Annotator) visitGoStmt(ctx *Ctx, gs *ast.GoStmt) {
	ann.visitDeferCallStmt(ctx, &gs.Call)
}

func (ann *Annotator) visitSelectStmt(ctx *Ctx, ss *ast.SelectStmt) {
	ann.visitBlockStmt(ctx, ss.Body)
}

//----------

func (ann *Annotator) visitCaseClause(ctx *Ctx, cc *ast.CaseClause) {
	ann.visitStmtList(ctx, &cc.Body)
}

func (ann *Annotator) visitCommClause(ctx *Ctx, cc *ast.CommClause) {
	// TODO: cc.Comm stmt ?
	ann.visitStmtList(ctx, &cc.Body)
}

//----------

func (ann *Annotator) visitSpec(ctx *Ctx, spec ast.Spec) {
	// specs: import, value, type

	switch t := spec.(type) {
	case *ast.ValueSpec:
		if len(t.Values) > 0 {
			// Ex: var a,b int = 1, 2; var a, b = f()
			// use an assignstmt

			lhs := []ast.Expr{}
			for _, id := range t.Names {
				lhs = append(lhs, id)
			}

			as := &ast.AssignStmt{
				Lhs:    lhs,
				TokPos: t.Pos(),
				Tok:    token.ASSIGN,
				Rhs:    t.Values,
			}
			ann.visitAssignStmt(ctx, as)
		}
	}
}

//----------

func (ann *Annotator) wrapInBlockStmt(ctx *Ctx, stmt ast.Stmt) *ast.BlockStmt {
	bs := &ast.BlockStmt{List: []ast.Stmt{stmt}}
	ctx.replaceStmt(bs)
	return bs
}

//----------

func (ann *Annotator) visitCallExpr(ctx *Ctx, ce *ast.CallExpr) {
	ctx = ctx.withInsertStmtAfter(false)

	// stepping in function name
	// also: first arg is type in case of new/make functions
	ctx2 := ctx
	fname := "f"
	switch t := ce.Fun.(type) {
	case *ast.Ident:
		fname = t.Name
		switch fname {
		case "new", "make":
			ctx2 = ctx2.withFirstArgIsType()
		}
	case *ast.SelectorExpr:
		fname = t.Sel.Name
	case *ast.FuncLit:
		pos := ce.Fun.End()
		e := ann.visitExpr(ctx, &ce.Fun)
		stmt := ann.newDebugLineStmt(ctx, pos, e)
		ctx.insertInStmtList(stmt)
	}
	fnamee := basicLitStringQ(fname)

	// visit args
	ctx2 = ctx2.withNResults(1)
	ctx2 = ctx2.withResultInVar()
	args := ann.visitExprList(ctx2, &ce.Args)

	// insert before calling the function (shows stepping in)
	args2 := append([]ast.Expr{fnamee}, args...)
	ce4 := ann.newDebugCallExpr("ICe", args2...)
	ctx3 := ctx.setupCallExprDebugIndex(ann)
	stmt := ann.newDebugLineStmt(ctx3, ce.Rparen, ce4)
	ctx.insertInStmtList(stmt)

	if fname == "panic" {
		// nil arg: newDebugLineStmt will generate an emptyStmt
		ctx.pushExprs(nil)
		return
	}

	ctx4 := ctx.withResultInVar()
	result := ann.getResultExpr(ctx4, ce)

	// args after exiting func
	args3 := append([]ast.Expr{fnamee, result}, args...)
	ce3 := ann.newDebugCallExpr("IC", args3...)
	id := ann.assignToNewIdent(ctx, ce3)
	ctx.pushExprs(id)
}

func (ann *Annotator) visitBinaryExpr(ctx *Ctx, be *ast.BinaryExpr) {
	ctx = ctx.withNResults(1)
	switch be.Op {
	case token.LAND, token.LOR:
		ann.visitBinaryExpr3(ctx, be)
	default:
		ann.visitBinaryExpr2(ctx, be)
	}
}
func (ann *Annotator) visitBinaryExpr2(ctx *Ctx, be *ast.BinaryExpr) {
	// keep isdirect before visiting expr
	// ex: "a:=1*f()" is not direct, but "d0:=f();a:=1*d0" is because d0 is an ident (that could refer to a const)
	direct := isDirectExpr(be)

	x := ann.visitExpr(ctx, &be.X)
	y := ann.visitExpr(ctx, &be.Y)

	ctx2 := ctx
	if direct {
		ctx2 = ctx2.withNoResultInVar()
	} else {
		ctx2 = ctx2.withResultInVar()
	}
	result := ann.getResultExpr(ctx2, be)

	opbl := basicLitInt(int(be.Op))
	ce3 := ann.newDebugCallExpr("IB", result, opbl, x, y)
	id1 := ann.assignToNewIdent(ctx, ce3)
	ctx.pushExprs(id1)
}

func (ann *Annotator) visitBinaryExpr3(ctx *Ctx, be *ast.BinaryExpr) {
	// ex: f1() || f2() // f2 should not be called if f1 is true
	// ex: f1() && f2() // f2 should not be called if f1 is false

	x := ann.visitExpr(ctx, &be.X)

	// y if be.Y doesn't run
	q := ann.newDebugCallExpr("IVs", basicLitStringQ("?"))
	y := ann.assignToNewIdent(ctx, q)

	// create final result variable, initially with be.X
	ctx2 := ctx.withInsertStmtAfter(false)
	finalResult := ann.assignToNewIdent(ctx2, be.X)
	ctx.replaceExpr(finalResult)

	// create ifstmt to run be.Y if be.X is true
	var xcond ast.Expr = finalResult // token.LAND
	if be.Op == token.LOR {
		xcond = &ast.UnaryExpr{Op: token.NOT, X: xcond}
	}
	is := &ast.IfStmt{If: be.Pos(), Cond: xcond, Body: &ast.BlockStmt{}}
	ctx.insertInStmtListBefore(is)

	// (inside ifstmt) assign be.Y to result variable
	as2 := ann.newAssignStmt11(finalResult, be.Y)
	as2.Tok = token.ASSIGN
	is.Body.List = append(is.Body.List, as2)

	// (inside ifstmt) run be.Y
	ctx3, _ := ctx.withStmtIter(&is.Body.List) // index at 0
	y2 := ann.visitExpr(ctx3, &as2.Rhs[0])

	// (inside ifstmt) assign debug result to y
	as3 := ann.newAssignStmt11(y, y2)
	as3.Tok = token.ASSIGN
	is.Body.List = append(is.Body.List, as3)

	result := ann.newDebugCallExpr("IV", finalResult)

	opbl := basicLitInt(int(be.Op))
	ce3 := ann.newDebugCallExpr("IB", result, opbl, x, y)
	id1 := ann.assignToNewIdent(ctx, ce3)
	ctx.pushExprs(id1)
}

func (ann *Annotator) visitUnaryExpr(ctx *Ctx, ue *ast.UnaryExpr) {

	// X expression
	ctx2 := ctx
	switch ue.Op {
	case token.AND:
		// Ex: f1(&c[i]) -> d0:=c[i]; f1(&d0) // d0 wrong address
		ctx2 = ctx.withNoResultInVar()
	}
	ctx2 = ctx2.withNResults(1)
	x := ann.visitExpr(ctx2, &ue.X)

	ctx3 := ctx
	direct := isDirectExpr(ue)
	if direct {
		ctx3 = ctx3.withNoResultInVar()
	} else {
		ctx3 = ctx3.withResultInVar()
	}
	result := ann.getResultExpr(ctx3, ue)

	opbl := basicLitInt(int(ue.Op))
	ce3 := ann.newDebugCallExpr("IU", result, opbl, x)
	id := ann.assignToNewIdent(ctx, ce3)
	ctx.pushExprs(id)
}

func (ann *Annotator) visitSelectorExpr(ctx *Ctx, se *ast.SelectorExpr) {
	ce := ann.newDebugCallExpr("IV", se)
	id := ann.assignToNewIdent(ctx, ce)
	ctx.pushExprs(id)
}

func (ann *Annotator) visitIndexExpr(ctx *Ctx, ie *ast.IndexExpr) {
	// ex: a, ok := c[f1()] // map access, more then 1 result
	// ex: a, b = c[i], d[j]

	// X expr
	var x ast.Expr
	switch ie.X.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		x = nilIdent() // direct nil
	default:
		x = ann.visitExpr(ctx, &ie.X)
	}

	// Index expr
	ctx2 := ctx.withResultInVar()
	ctx2 = ctx2.withNResults(1) // a = b[f()] // f() returns 1 result
	ix := ann.visitExpr(ctx2, &ie.Index)

	result := ann.getResultExpr(ctx, ie)

	ce3 := ann.newDebugCallExpr("II", result, x, ix)
	ctx.pushExprs(ce3)
}

func (ann *Annotator) visitSliceExpr(ctx *Ctx, se *ast.SliceExpr) {
	var x ast.Expr
	switch se.X.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		x = nilIdent()
	default:
		x = ann.visitExpr(ctx, &se.X)
	}

	var ix []ast.Expr
	for _, e := range []*ast.Expr{&se.Low, &se.High, &se.Max} {
		var r ast.Expr
		if *e != nil {
			r = ann.visitExpr(ctx, e)
		}
		if r == nil {
			r = nilIdent() // direct nil
		}
		ix = append(ix, r)
	}

	result := ann.getResultExpr(ctx, se)

	// slice3: 2 colons present
	s := "false"
	if se.Slice3 {
		s = "true"
	}
	bl := basicLitString(s)

	ce := ann.newDebugCallExpr("II2", result, x, ix[0], ix[1], ix[2], bl)
	ctx.pushExprs(ce)
}

func (ann *Annotator) visitKeyValueExpr(ctx *Ctx, kv *ast.KeyValueExpr) {
	var k ast.Expr
	if id, ok := kv.Key.(*ast.Ident); ok {
		k = ann.newDebugCallExpr("IVs", basicLitStringQ(id.Name))
	} else {
		k = ann.visitExpr(ctx, &kv.Key)
	}

	v := ann.visitExpr(ctx, &kv.Value)

	ce := ann.newDebugCallExpr("KV", k, v)
	ctx.pushExprs(ce)
}

func (ann *Annotator) visitTypeAssertExpr(ctx *Ctx, tae *ast.TypeAssertExpr) {
	ce := ann.newDebugCallExpr("IVt", tae.X)
	ctx.pushExprs(ce)
}

func (ann *Annotator) visitParenExpr(ctx *Ctx, pe *ast.ParenExpr) {
	x := ann.visitExpr(ctx, &pe.X)
	ce := ann.newDebugCallExpr("IP", x)
	ctx.pushExprs(ce)
}

func (ann *Annotator) visitStarExpr(ctx *Ctx, se *ast.StarExpr) {
	// Ex: *a=1
	ctx = ctx.withNResults(1)

	x := ann.visitExpr(ctx, &se.X)

	ctx3 := ctx.withNoResultInVar()
	result := ann.getResultExpr(ctx3, se)

	opbl := basicLitInt(int(token.MUL))
	ce3 := ann.newDebugCallExpr("IU", result, opbl, x)
	id := ann.assignToNewIdent(ctx, ce3)
	ctx.pushExprs(id)
}

//----------

func (ann *Annotator) visitBasicLit(ctx *Ctx, bl *ast.BasicLit) {
	switch bl.Kind {
	case token.STRING:
		ctx2 := ctx.withInsertStmtAfter(false) // a["s"]=1 -> d0:="s";a[d0]=1
		id := ann.assignToNewIdent(ctx2, bl)
		ctx.replaceExpr(id)
		ce := ann.newDebugCallExpr("IV", id)
		id2 := ann.assignToNewIdent(ctx2, ce)
		ctx.pushExprs(id2)
		return
	}

	ce := ann.newDebugCallExpr("IV", bl)
	id := ann.assignToNewIdent(ctx, ce)
	ctx.pushExprs(id)
}

func (ann *Annotator) visitFuncLit(ctx *Ctx, fl *ast.FuncLit) {
	ctx = ctx.valuesReset()

	id := ann.assignToNewIdent(ctx, fl)
	ctx.replaceExpr(id)

	ctx2 := ctx.withFuncType(fl.Type)
	ann.visitBlockStmt(ctx2, fl.Body)

	ce := ann.newDebugCallExpr("IV", id)
	id2 := ann.assignToNewIdent(ctx, ce)
	ctx.pushExprs(id2)
}

func (ann *Annotator) visitCompositeLit(ctx *Ctx, cl *ast.CompositeLit) {
	u := ann.visitExprList(ctx, &cl.Elts)
	ce := ann.newDebugCallExpr("ILit", u...)
	ctx.pushExprs(ce)
}

//----------

func (ann *Annotator) visitIdent(ctx *Ctx, id *ast.Ident) {
	if isAnonIdent(id) {
		ce := ann.newDebugCallExpr("IAn")
		ctx.pushExprs(ce)
		return
	}
	ce := ann.newDebugCallExpr("IV", id)
	id2 := ann.assignToNewIdent(ctx, ce)
	ctx.pushExprs(id2)
}

//----------

//func (ann *Annotator) visitArrayType(ctx *Ctx, at *ast.ArrayType) {
//	e := ann.visitType(ctx)
//	ctx.pushExprs(e)
//}

func (ann *Annotator) visitType(ctx *Ctx) ast.Expr {
	bl := basicLitStringQ("type")
	ce := ann.newDebugCallExpr("IVs", bl)
	id := ann.assignToNewIdent(ctx, ce)
	return id
}

//----------

func (ann *Annotator) visitFieldList(ctx *Ctx, fl *ast.FieldList) []ast.Expr {
	exprs := []ast.Expr{}
	for _, f := range fl.List {
		w := ann.visitField(ctx, f)
		exprs = append(exprs, w...)
	}
	return exprs
}

func (ann *Annotator) visitField(ctx *Ctx, field *ast.Field) []ast.Expr {
	ctx2 := ctx.withNewExprs()
	exprs := []ast.Expr{}
	for _, id := range field.Names {
		ann.visitIdent(ctx2, id)
		w := ctx2.popExprs()
		exprs = append(exprs, w...)
	}
	return exprs
}

//----------

func (ann *Annotator) visitStmtList(ctx *Ctx, list *[]ast.Stmt) {
	ctx2, iter := ctx.withStmtIter(list)

	for iter.index < len(*list) {
		stmt := (*list)[iter.index]

		// stmts defaults
		ctx3 := ctx2
		switch stmt.(type) {
		case *ast.ExprStmt, *ast.AssignStmt:
			// ast.SwitchStmt needs false
			ctx3 = ctx3.withInsertStmtAfter(true)
		}

		ann.visitStmt(ctx3, stmt)

		iter.index += 1 + iter.step
		iter.step = 0
	}
}

func (ann *Annotator) visitStmt(ctx *Ctx, stmt ast.Stmt) {
	ctx = ctx.withNewExprs()
	ctx = ctx.withNResults(0)
	ctx = ctx.withNoStaticDebugIndex()
	ctx = ctx.withCallExprDebugIndex()

	switch t := stmt.(type) {
	case *ast.ExprStmt:
		ann.visitExprStmt(ctx, t)
	case *ast.AssignStmt:
		ann.visitAssignStmt(ctx, t)
	case *ast.TypeSwitchStmt:
		ann.visitTypeSwitchStmt(ctx, t)
	case *ast.SwitchStmt:
		ann.visitSwitchStmt(ctx, t)
	case *ast.IfStmt:
		ann.visitIfStmt(ctx, t)
	case *ast.ForStmt:
		ann.visitForStmt(ctx, t)
	case *ast.RangeStmt:
		ann.visitRangeStmt(ctx, t)
	case *ast.LabeledStmt:
		ann.visitLabeledStmt(ctx, t)
	case *ast.ReturnStmt:
		ann.visitReturnStmt(ctx, t)
	case *ast.DeferStmt:
		ann.visitDeferStmt(ctx, t)
	case *ast.DeclStmt:
		ann.visitDeclStmt(ctx, t)
	case *ast.BranchStmt:
		ann.visitBranchStmt(ctx, t)
	case *ast.IncDecStmt:
		ann.visitIncDecStmt(ctx, t)
	case *ast.SendStmt:
		ann.visitSendStmt(ctx, t)
	case *ast.GoStmt:
		ann.visitGoStmt(ctx, t)
	case *ast.SelectStmt:
		ann.visitSelectStmt(ctx, t)
	case *ast.BlockStmt:
		ann.visitBlockStmt(ctx, t)

	case *ast.CaseClause:
		ann.visitCaseClause(ctx, t)
	case *ast.CommClause:
		ann.visitCommClause(ctx, t)

	default:
		spew.Dump("stmt", t)
	}
}

func (ann *Annotator) visitExprList(ctx *Ctx, list *[]ast.Expr) []ast.Expr {
	var exprs []ast.Expr
	ctx2, iter := ctx.withExprIter(list)
	for iter.index < len(*list) {
		exprPtr := &(*list)[iter.index]

		if iter.index == 0 && ctx.firstArgIsType() {
			e := ann.visitType(ctx)
			exprs = append(exprs, e)
		} else {
			e := ann.visitExpr(ctx2, exprPtr)
			exprs = append(exprs, e)
		}

		iter.index += 1 + iter.step
		iter.step = 0
	}
	return exprs
}

func (ann *Annotator) visitExpr(ctx *Ctx, exprPtr *ast.Expr) ast.Expr {
	//fmt.Printf("visitExpr: %T\n", *exprPtr)

	ctx = ctx.withNewExprs()
	ctx = ctx.withExprPtr(exprPtr)

	switch t := (*exprPtr).(type) {
	case *ast.CallExpr:
		ann.visitCallExpr(ctx, t)
	case *ast.BinaryExpr:
		ann.visitBinaryExpr(ctx, t)
	case *ast.UnaryExpr:
		ann.visitUnaryExpr(ctx, t)
	case *ast.SelectorExpr:
		ann.visitSelectorExpr(ctx, t)
	case *ast.IndexExpr:
		ann.visitIndexExpr(ctx, t)
	case *ast.SliceExpr:
		ann.visitSliceExpr(ctx, t)
	case *ast.KeyValueExpr:
		ann.visitKeyValueExpr(ctx, t)
	case *ast.TypeAssertExpr:
		ann.visitTypeAssertExpr(ctx, t)
	case *ast.ParenExpr:
		ann.visitParenExpr(ctx, t)
	case *ast.StarExpr:
		ann.visitStarExpr(ctx, t)

	case *ast.BasicLit:
		ann.visitBasicLit(ctx, t)
	case *ast.FuncLit:
		ann.visitFuncLit(ctx, t)
	case *ast.CompositeLit:
		ann.visitCompositeLit(ctx, t)

	case *ast.Ident:
		ann.visitIdent(ctx, t)

		//	case *ast.ArrayType:
		//		ann.visitArrayType(ctx, t)

	default:
		spew.Dump("expr", t)
		//panic("!")
	}

	exprs := ctx.popExprs()
	if len(exprs) == 1 {
		return exprs[0]
	}

	spew.Dump("visitExpr: len=", len(exprs))
	return nilIdent()
	//return ann.newDebugCallExpr("IV", nilIdent())
}

//----------

func (ann *Annotator) getResultExpr(ctx *Ctx, e ast.Expr) ast.Expr {
	nres := ctx.nResults()
	if nres == 0 {
		return nilIdent()
	}
	if nres >= 2 {
		u := ann.assignToNewIdents(ctx, nres, e)
		ctx.replaceExprs(u)

		var u2 []ast.Expr
		for _, e := range u {
			ce := ann.newDebugCallExpr("IV", e)
			u2 = append(u2, ce)
		}

		ce := ann.newDebugCallExpr("IL", u2...)
		return ann.assignToNewIdent(ctx, ce)
	}

	// nres == 1
	//if isDirectExpr(e) {
	// never put the result in a variable if it is a direct expr
	//} else
	if ctx.resultInVar() {
		// putting the result in a variable is never inserted after
		ctx = ctx.withInsertStmtAfter(false)
		e = ann.assignToNewIdent(ctx, e)
		ctx.replaceExpr(e)
	}
	//else {
	// do nothing
	//}

	ce := ann.newDebugCallExpr("IV", e)
	return ann.assignToNewIdent(ctx, ce)
}

//----------

func (ann *Annotator) assignToNewIdent(ctx *Ctx, e ast.Expr) ast.Expr {
	u := ann.assignToNewIdents(ctx, 1, e)
	return u[0]
}

func (ann *Annotator) assignToNewIdents(ctx *Ctx, nids int, exprs ...ast.Expr) []ast.Expr {
	ids := []ast.Expr{}
	for i := 0; i < nids; i++ {
		ids = append(ids, ann.newIdent())
	}
	stmt := ann.newAssignStmt(ids, exprs)
	ctx.insertInStmtList(stmt)
	return ids
}

//----------

func (ann *Annotator) newDebugCallExpr(fname string, u ...ast.Expr) *ast.CallExpr {
	se := &ast.SelectorExpr{
		X:   ast.NewIdent(ann.debugPkgName),
		Sel: ast.NewIdent(fname),
	}
	return &ast.CallExpr{Fun: se, Args: u}
}

func (ann *Annotator) newDebugLineStmt(ctx *Ctx, pos token.Pos, e ast.Expr) ast.Stmt {
	if e == nil {
		return &ast.EmptyStmt{}
	}

	ann.builtDebugLineStmt = true

	// debug index
	ctx2 := ctx.callExprDebugIndex()
	var di int
	if i, ok := ctx2.staticDebugIndex(); ok {
		di = i
	} else {
		di = ann.debugIndex
		ann.debugIndex++
	}

	position := ann.fset.Position(pos)
	lineOffset := position.Offset

	args := []ast.Expr{
		basicLitInt(ann.fileIndex),
		basicLitInt(di),
		basicLitInt(lineOffset),
		e,
	}

	se := &ast.SelectorExpr{
		X:   ast.NewIdent(ann.debugPkgName),
		Sel: ast.NewIdent("Line"),
	}
	es := &ast.ExprStmt{X: &ast.CallExpr{Fun: se, Args: args}}
	return es
}

//----------

func (ann *Annotator) newIdent() *ast.Ident {
	return &ast.Ident{Name: ann.newVarName()}
}
func (ann *Annotator) newVarName() string {
	defer func() { ann.debugVarNameIndex++ }()
	return fmt.Sprintf(ann.debugVarPrefix+"%d", ann.debugVarNameIndex)
}

func (ann *Annotator) newAssignStmt11(lhs, rhs ast.Expr) *ast.AssignStmt {
	return &ast.AssignStmt{Tok: token.DEFINE, Lhs: []ast.Expr{lhs}, Rhs: []ast.Expr{rhs}}
}
func (ann *Annotator) newAssignStmt(lhs, rhs []ast.Expr) *ast.AssignStmt {
	return &ast.AssignStmt{Tok: token.DEFINE, Lhs: lhs, Rhs: rhs}
}

//----------

var _nilIdent = &ast.Ident{Name: "nil"}

func nilIdent() *ast.Ident {
	return _nilIdent
}

func anonIdent() *ast.Ident {
	return &ast.Ident{Name: "_"}
}
func isAnonIdent(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "_"
}

//----------

func basicLitString(v string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: v}
}
func basicLitStringQ(v string) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.STRING, Value: fmt.Sprintf("%q", v)}
}
func basicLitInt(v int) *ast.BasicLit {
	return &ast.BasicLit{Kind: token.INT, Value: fmt.Sprintf("%d", v)}
}

//----------

// Can't create vars from the expr or it could create a var of different type.
func isDirectExpr(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return isDirectExpr(t.X)
	case *ast.UnaryExpr:
		switch t.Op {
		case token.ADD, // +
			token.SUB, // -
			token.XOR: // ^
			return isDirectExpr(t.X)
		}
	case *ast.BasicLit:
		switch t.Kind {
		case token.CHAR: // 'c' can be assigned to {byte,rune,...}
			return true
		case token.INT, token.FLOAT:
			return true
		}
	case *ast.Ident, *ast.SelectorExpr:
		//switch t.Name {
		//case "nil": // always true
		//	return true
		//}

		// if "a" is const, it would be type int if assigned to a tmp var
		// ex: var a int32 = 0 | a
		// ex: f(1+a) with f=func(int32)

		return true
	case *ast.BinaryExpr:
		// ex: var a float =1*2 // (1*2) gives type int if assigned to a tmp var
		switch t.Op {
		case token.ADD, // +
			token.SUB,     // -
			token.MUL,     // *
			token.QUO,     // /
			token.REM,     // %
			token.AND,     // &
			token.OR,      // |
			token.XOR,     // ^
			token.SHL,     // <<
			token.SHR,     // >>
			token.AND_NOT: // &^
			return isDirectExpr(t.X) && isDirectExpr(t.Y)
		}
	}
	return false
}
