package goutil

import (
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"

	"github.com/jmigpin/editor/util/osutil"
)

func GoPath() []string {
	// TODO: use go/build defaultgopath if it becomes public
	a := []string{}
	gopath := os.Getenv("GOPATH")
	if gopath != "" {
		a = append(a, filepath.SplitList(gopath)...)
	} else {
		// from go/build/build.go:274
		a = append(a, filepath.Join(osutil.HomeEnvVar(), "go"))
	}
	return a
}

func JoinPathLists(w ...string) string {
	return strings.Join(w, string(os.PathListSeparator))
}

//----------

//func ExtractSrcDir(filename string) (string, string) {
//	srcDir := ""
//	for _, d := range build.Default.SrcDirs() {
//		d += string(filepath.Separator)
//		if strings.HasPrefix(filename, d) {
//			srcDir = filename[:len(d)]
//			filename = filename[len(d):]
//			return srcDir, filename
//		}
//	}
//	return srcDir, filename
//}

//----------

func AstFileFilename(astFile *ast.File, fset *token.FileSet) (string, error) {
	if astFile == nil {
		panic("!")
	}
	tfile := fset.File(astFile.Package)
	if tfile == nil {
		return "", fmt.Errorf("not found")
	}
	return tfile.Name(), nil
}

//----------

func PrintAstFile(w io.Writer, fset *token.FileSet, astFile *ast.File) error {
	// TODO: without tabwidth set, it won't output the source correctly

	// print with source positions from original file

	// Fail: has struct fields without spaces "field int"->"fieldint"
	//cfg := &printer.Config{Mode: printer.SourcePos | printer.TabIndent}

	// Fail: has stmts split with comments in the middle
	//cfg := &printer.Config{Mode: printer.SourcePos | printer.TabIndent | printer.UseSpaces}

	cfg := &printer.Config{Mode: printer.SourcePos, Tabwidth: 4}

	return cfg.Fprint(w, fset, astFile)
}

//----------

func Printfc(skip int, f string, args ...interface{}) {
	pc, _, _, ok := runtime.Caller(1 + skip)
	details := runtime.FuncForPC(pc)
	if ok && details != nil {
		u := details.Name()
		i := strings.Index(u, "(")
		if i > 0 {
			u = u[i:]
		}
		fmt.Printf(u+": "+f, args...)
		return
	}
	fmt.Printf(f, args...)
}

//----------

// go test -cpuprofile cpu.prof -memprofile mem.prof
// go tool pprof cpu.prof
// view with a browser:
// go tool pprof -http=:8000 cpu.prof

var profFile *os.File

func StartCPUProfile() error {
	filename := "cpu.prof"
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	profFile = f
	log.Printf("profile cpu: %v\n", filename)
	return pprof.StartCPUProfile(f)
}

func StopCPUProfile() error {
	pprof.StopCPUProfile()
	return profFile.Close()
}
