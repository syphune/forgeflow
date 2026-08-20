package intelligence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Config struct {
	MaxFiles      int
	MaxFileBytes  int64
	MaxTotalBytes int64
}

type File struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	Size        int64  `json:"size"`
	ContentHash string `json:"content_hash"`
}

type Symbol struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Qualified  string `json:"qualified_name"`
	Kind       string `json:"kind"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	Confidence string `json:"confidence"`
	Provenance string `json:"provenance"`
}

type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
	Provenance string `json:"provenance"`
}

type Snapshot struct {
	Root      string   `json:"root"`
	CommitSHA string   `json:"commit_sha"`
	Files     []File   `json:"files"`
	Symbols   []Symbol `json:"symbols"`
	Edges     []Edge   `json:"edges"`
	Skipped   []string `json:"skipped,omitempty"`
	contents  map[string][]byte
}

type Indexer struct{ config Config }

func NewIndexer(config Config) *Indexer {
	if config.MaxFiles <= 0 {
		config.MaxFiles = 20_000
	}
	if config.MaxFileBytes <= 0 {
		config.MaxFileBytes = 2 << 20
	}
	if config.MaxTotalBytes <= 0 {
		config.MaxTotalBytes = 256 << 20
	}
	return &Indexer{config: config}
}

func (i *Indexer) Index(ctx context.Context, root, commitSHA string) (*Snapshot, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat repository root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository root is not a directory")
	}
	snapshot := &Snapshot{Root: root, CommitSHA: commitSHA, Files: make([]File, 0), Symbols: make([]Symbol, 0), Edges: make([]Edge, 0), contents: make(map[string][]byte)}
	var total int64
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		first := strings.Split(rel, "/")[0]
		if first == ".git" || first == "node_modules" || first == "vendor" || first == ".next" || first == "dist" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			snapshot.Skipped = append(snapshot.Skipped, rel)
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if len(snapshot.Files) >= i.config.MaxFiles {
			snapshot.Skipped = append(snapshot.Skipped, rel)
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !fileInfo.Mode().IsRegular() || fileInfo.Size() > i.config.MaxFileBytes || total+fileInfo.Size() > i.config.MaxTotalBytes {
			snapshot.Skipped = append(snapshot.Skipped, rel)
			return nil
		}
		content, err := readBounded(path, i.config.MaxFileBytes)
		if err != nil {
			return err
		}
		total += int64(len(content))
		digest := sha256.Sum256(content)
		file := File{Path: rel, Language: language(rel), Size: int64(len(content)), ContentHash: hex.EncodeToString(digest[:])}
		snapshot.Files = append(snapshot.Files, file)
		snapshot.contents[rel] = content
		i.parse(rel, content, snapshot)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("index repository: %w", err)
	}
	sort.Slice(snapshot.Files, func(a, b int) bool { return snapshot.Files[a].Path < snapshot.Files[b].Path })
	sort.Slice(snapshot.Symbols, func(a, b int) bool {
		if snapshot.Symbols[a].Path == snapshot.Symbols[b].Path {
			return snapshot.Symbols[a].StartLine < snapshot.Symbols[b].StartLine
		}
		return snapshot.Symbols[a].Path < snapshot.Symbols[b].Path
	})
	snapshot.Edges = deduplicateEdges(snapshot.Edges)
	return snapshot, nil
}

// IndexFiles indexes an already bounded, fixed-ref file set. The caller owns
// fetching and validating the source; this method keeps parsing independent of
// the local filesystem so remote repository snapshots can use the same rules.
func (i *Indexer) IndexFiles(ctx context.Context, commitSHA string, files map[string][]byte) (*Snapshot, error) {
	if strings.TrimSpace(commitSHA) == "" {
		return nil, fmt.Errorf("commit SHA is required")
	}
	snapshot := &Snapshot{CommitSHA: strings.TrimSpace(commitSHA), Files: make([]File, 0), Symbols: make([]Symbol, 0), Edges: make([]Edge, 0), contents: make(map[string][]byte)}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var total int64
	for _, rawPath := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := safeRemotePath(rawPath)
		if err != nil {
			snapshot.Skipped = append(snapshot.Skipped, rawPath)
			continue
		}
		if len(snapshot.Files) >= i.config.MaxFiles {
			snapshot.Skipped = append(snapshot.Skipped, path)
			continue
		}
		content := files[rawPath]
		if int64(len(content)) > i.config.MaxFileBytes || total+int64(len(content)) > i.config.MaxTotalBytes {
			snapshot.Skipped = append(snapshot.Skipped, path)
			continue
		}
		content = append([]byte(nil), content...)
		total += int64(len(content))
		digest := sha256.Sum256(content)
		snapshot.Files = append(snapshot.Files, File{Path: path, Language: language(path), Size: int64(len(content)), ContentHash: hex.EncodeToString(digest[:])})
		snapshot.contents[path] = content
		i.parse(path, content, snapshot)
	}
	sort.Slice(snapshot.Files, func(a, b int) bool { return snapshot.Files[a].Path < snapshot.Files[b].Path })
	sort.Slice(snapshot.Symbols, func(a, b int) bool {
		if snapshot.Symbols[a].Path == snapshot.Symbols[b].Path {
			return snapshot.Symbols[a].StartLine < snapshot.Symbols[b].StartLine
		}
		return snapshot.Symbols[a].Path < snapshot.Symbols[b].Path
	})
	snapshot.Edges = deduplicateEdges(snapshot.Edges)
	return snapshot, nil
}

func (s *Snapshot) GetFile(path string) ([]byte, error) {
	rel, err := safeRelative(path)
	if err != nil {
		return nil, err
	}
	content, ok := s.contents[rel]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), content...), nil
}

func (s *Snapshot) Search(query string, limit int) []File {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	result := make([]File, 0, limit)
	for _, file := range s.Files {
		if strings.Contains(strings.ToLower(string(s.contents[file.Path])), query) {
			result = append(result, file)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func (s *Snapshot) SymbolsNamed(name string, limit int) []Symbol {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	name = strings.ToLower(strings.TrimSpace(name))
	result := make([]Symbol, 0, limit)
	for _, symbol := range s.Symbols {
		if strings.ToLower(symbol.Name) == name || strings.ToLower(symbol.Qualified) == name {
			result = append(result, symbol)
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func (i *Indexer) parse(path string, content []byte, snapshot *Snapshot) {
	switch language(path) {
	case "go":
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, content, parser.ParseComments)
		if err != nil {
			return
		}
		for _, spec := range file.Imports {
			module, err := strconv.Unquote(spec.Path.Value)
			if err != nil || strings.TrimSpace(module) == "" {
				continue
			}
			snapshot.Edges = append(snapshot.Edges, Edge{From: path, To: module, Kind: "import", Confidence: "candidate", Provenance: "EXTRACTED"})
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var name, kind string
			switch item := node.(type) {
			case *ast.FuncDecl:
				name = item.Name.Name
				kind = "function"
			case *ast.TypeSpec:
				name = item.Name.Name
				kind = "type"
			case *ast.ValueSpec:
				kind = "value"
				for _, ident := range item.Names {
					name = ident.Name
					addSymbol(path, name, kind, item.Pos(), item.End(), fset, snapshot)
				}
			}
			if name != "" && kind != "value" {
				addSymbol(path, name, kind, node.Pos(), node.End(), fset, snapshot)
			}
			return true
		})
	case "typescript", "javascript":
		source := string(content)
		for _, expression := range []*regexp.Regexp{moduleImportPattern, moduleExportPattern, requirePattern} {
			for _, match := range expression.FindAllStringSubmatch(source, -1) {
				if len(match) != 2 || strings.TrimSpace(match[1]) == "" {
					continue
				}
				snapshot.Edges = append(snapshot.Edges, Edge{From: path, To: match[1], Kind: "import", Confidence: "candidate", Provenance: "EXTRACTED"})
			}
		}
		line, cursor := 1, 0
		for _, match := range symbolPattern.FindAllStringSubmatchIndex(source, -1) {
			line += strings.Count(source[cursor:match[0]], "\n")
			cursor = match[0]
			kind := source[match[2]:match[3]]
			name := source[match[4]:match[5]]
			snapshot.Symbols = append(snapshot.Symbols, Symbol{Path: path, Name: name, Qualified: name, Kind: strings.ToLower(kind), StartLine: line, EndLine: line, Confidence: "candidate", Provenance: "EXTRACTED"})
		}
	}
}

var symbolPattern = regexp.MustCompile(`(?m)\b(function|class|interface|type|const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)`)

// ponytail: bounded regex extraction keeps the static CGO-free build; replace
// these expressions with Tree-sitter when exact TS/JS module resolution is a
// measured product requirement.
var (
	moduleImportPattern = regexp.MustCompile(`(?m)\bimport\s+(?:[^;\n]*?\s+from\s+)?["']([^"']+)["']`)
	moduleExportPattern = regexp.MustCompile(`(?m)\bexport\s+[^;\n]*?\s+from\s+["']([^"']+)["']`)
	requirePattern      = regexp.MustCompile(`\brequire\s*\(\s*["']([^"']+)["']\s*\)`)
)

func deduplicateEdges(edges []Edge) []Edge {
	seen := make(map[string]struct{}, len(edges))
	result := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		key := strings.Join([]string{edge.From, edge.To, edge.Kind}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, edge)
	}
	sort.Slice(result, func(a, b int) bool {
		if result[a].From == result[b].From {
			if result[a].To == result[b].To {
				return result[a].Kind < result[b].Kind
			}
			return result[a].To < result[b].To
		}
		return result[a].From < result[b].From
	})
	return result
}

func addSymbol(path, name, kind string, start, end token.Pos, fset *token.FileSet, snapshot *Snapshot) {
	sp, ep := fset.Position(start), fset.Position(end)
	snapshot.Symbols = append(snapshot.Symbols, Symbol{Path: path, Name: name, Qualified: name, Kind: kind, StartLine: sp.Line, EndLine: ep.Line, Confidence: "proven", Provenance: "EXTRACTED"})
}
func readBounded(path string, max int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > max {
		return nil, fmt.Errorf("file exceeds configured limit")
	}
	return content, nil
}
func language(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx", ".mjs", ".cjs":
		return "javascript"
	case ".py":
		return "python"
	case ".java":
		return "java"
	default:
		return "text"
	}
}
func safeRelative(path string) (string, error) {
	if filepath.IsAbs(path) || !filepath.IsLocal(path) {
		return "", fmt.Errorf("unsafe repository path")
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("unsafe repository path")
	}
	return clean, nil
}

func safeRemotePath(path string) (string, error) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	if path == "" || strings.HasPrefix(path, "/") || hasParentSegment(path) {
		return "", fmt.Errorf("unsafe repository path")
	}
	clean := strings.TrimPrefix(path, "./")
	if clean == "" || clean == "." || strings.Contains(clean, "//") {
		return "", fmt.Errorf("unsafe repository path")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("unsafe repository path")
		}
	}
	return clean, nil
}

func hasParentSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}
