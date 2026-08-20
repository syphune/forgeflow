package intelligence

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIndexerBoundsAndExtractsSymbols(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\nfunc Run() {}\ntype Config struct{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.ts"), []byte("const value = 1;\nexport function boot() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewIndexer(Config{MaxFiles: 10, MaxFileBytes: 1024, MaxTotalBytes: 2048}).Index(context.Background(), root, "fixed-sha")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CommitSHA != "fixed-sha" || len(snapshot.Files) != 2 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if len(snapshot.SymbolsNamed("Run", 10)) != 1 || len(snapshot.SymbolsNamed("boot", 10)) != 1 || snapshot.SymbolsNamed("boot", 10)[0].StartLine != 2 {
		t.Fatalf("expected extracted symbols: %#v", snapshot.Symbols)
	}
	if len(snapshot.Search("package main", 10)) != 1 {
		t.Fatal("expected lexical match")
	}
	if _, err := snapshot.GetFile("../secret"); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestIndexerExtractsBoundedImportEdges(t *testing.T) {
	snapshot, err := NewIndexer(Config{MaxFiles: 10, MaxFileBytes: 1024, MaxTotalBytes: 4096}).IndexFiles(context.Background(), "fixed-sha", map[string][]byte{
		"cmd/main.go": []byte("package main\nimport (\n  \"fmt\"\n  \"example.com/app/internal/config\"\n)\nfunc main() { fmt.Println(config.Value) }\n"),
		"src/app.ts":  []byte("import { Button } from './ui/Button';\nconst value = require(\"./config\");\nexport { value } from './shared';\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Edges) != 5 {
		t.Fatalf("edges = %#v, want 5", snapshot.Edges)
	}
	for _, edge := range snapshot.Edges {
		if edge.Kind != "import" || edge.Provenance != "EXTRACTED" || edge.Confidence != "candidate" {
			t.Fatalf("edge provenance = %#v", edge)
		}
	}
}
