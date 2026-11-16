package envvar

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// literalEnvReads returns "file:line: os.Getenv("NAME")" for every call of
// os.Getenv or os.LookupEnv under root whose first argument is a string
// literal beginning VH_ or BOARD_, in a non-test .go file. The table's own
// file (skipFile, a path relative to root, "" for none) is not looked at.
func literalEnvReads(root, skipFile string) ([]string, error) {
	var found []string
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || rel == skipFile {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if name, line, ok := envReadWithLiteral(fset, n); ok {
				found = append(found, rel+":"+strconv.Itoa(line)+": "+name)
			}
			return true
		})
		return nil
	})
	return found, err
}

// envReadWithLiteral recognises os.Getenv("VH_…") and os.LookupEnv("BOARD_…").
func envReadWithLiteral(fset *token.FileSet, n ast.Node) (string, int, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) == 0 {
		return "", 0, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Getenv" && sel.Sel.Name != "LookupEnv") {
		return "", 0, false
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" {
		return "", 0, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", 0, false
	}
	name, err := strconv.Unquote(lit.Value)
	if err != nil || (!strings.HasPrefix(name, vhPrefix) && !strings.HasPrefix(name, boardPrefix)) {
		return "", 0, false
	}
	return "os." + sel.Sel.Name + "(" + lit.Value + ")", fset.Position(call.Pos()).Line, true
}

// TestNoProductCodePassesAVHOrBoardLiteralToTheOSEnvironment is the R1 static
// test: outside the table, no non-test file under cmd/ or internal/ reads a
// VH_ or BOARD_ name from the process environment.
func TestNoProductCodePassesAVHOrBoardLiteralToTheOSEnvironment(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{"cmd", "internal"} {
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			t.Fatalf("the repository root is not two levels up: %v", err)
		}
		found, err := literalEnvReads(filepath.Join(root, dir), "internal/envvar/envvar.go")
		if err != nil {
			t.Fatal(err)
		}
		if len(found) != 0 {
			t.Errorf("%s reads the environment with a literal name outside the table: %v (use internal/envvar)", dir, found)
		}
	}
}

// plantedTree copies testdata/planted into a temporary directory, dropping the
// .fixture suffix, so the planted files are .go files only while a test runs
// (as .go files in the tree they would be unmeasured product code).
func plantedTree(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("testdata", "planted")
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, strings.TrimSuffix(rel, ".fixture"))
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return err
		}
		return os.WriteFile(out, raw, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// TestTheStaticTestFailsOnAPlantedFixture proves the scan is not vacuous: the
// fixture plants one read of each shape, plus calls that are allowed.
func TestTheStaticTestFailsOnAPlantedFixture(t *testing.T) {
	found, err := literalEnvReads(plantedTree(t), "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`bad.go:8: os.Getenv("BOARD_URL")`,
		`bad.go:9: os.Getenv("VH_API_KEY")`,
		`bad.go:10: os.LookupEnv("BOARD_ACTOR")`,
		`bad.go:11: os.LookupEnv("VH_LISTEN")`,
		`nested/deep.go:6: os.Getenv("BOARD_DB_URL")`,
	}
	if strings.Join(found, "\n") != strings.Join(want, "\n") {
		t.Fatalf("scan found\n%s\nwant\n%s", strings.Join(found, "\n"), strings.Join(want, "\n"))
	}
}

func TestTheStaticScanSkipsTheNamedTableFile(t *testing.T) {
	found, err := literalEnvReads(plantedTree(t), "bad.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if strings.HasPrefix(f, "bad.go") {
			t.Fatalf("the skipped file was scanned: %v", found)
		}
	}
	if len(found) != 1 {
		t.Fatalf("found = %v, want only nested/deep.go", found)
	}
}
