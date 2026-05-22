package video

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestIVideoRepoFirstMethodIsWithTx(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "video.go", nil, 0)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}

	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "IVideoRepo" {
				continue
			}
			iface, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok || len(iface.Methods.List) == 0 {
				t.Fatalf("IVideoRepo is not a non-empty interface")
			}
			if got := iface.Methods.List[0].Names[0].Name; got != "WithTx" {
				t.Fatalf("first IVideoRepo method = %s, want WithTx", got)
			}
			return
		}
	}

	t.Fatalf("IVideoRepo not found")
}
