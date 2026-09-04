package app

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

//go:embed web/*
var webFiles embed.FS

const stateVersion = 1

type Options struct {
	Source, Target, Imports, Listen, StateFile string
}

type Rule struct {
	Path    string `json:"path"`
	Include bool   `json:"include"`
}

type FileRecord struct {
	Identity string `json:"identity"`
}

type State struct {
	Version   int                   `json:"version"`
	Rules     []Rule                `json:"rules"`
	Files     map[string]FileRecord `json:"files"`
	Wildcards []string              `json:"wildcards,omitempty"`
}

type server struct {
	mu        sync.Mutex
	source    string
	target    string
	statePath string
	state     State
	token     string
	instance  string
	imports   string
}

type treeNode struct {
	Path      string      `json:"path"`
	Name      string      `json:"name"`
	Directory bool        `json:"directory"`
	Included  bool        `json:"included"`
	Explicit  *bool       `json:"explicit,omitempty"`
	Children  []*treeNode `json:"children,omitempty"`
}

type action struct {
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Detail string `json:"detail,omitempty"`
}

type plan struct {
	Create []action `json:"create"`
	Remove []action `json:"remove"`
	Skip   []action `json:"skip"`
}

type collectRequest struct {
	Folder      string `json:"folder"`
	Pattern     string `json:"pattern"`
	Destination string `json:"destination"`
}

type createFolderRequest struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

func newPlan() plan {
	return plan{Create: []action{}, Remove: []action{}, Skip: []action{}}
}

func Run(opts Options) error {
	source, err := cleanExistingDir(opts.Source)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	target, err := cleanOrCreateDir(opts.Target)
	if err != nil {
		return fmt.Errorf("mirror: %w", err)
	}
	if source == target || isWithin(source, target) || isWithin(target, source) {
		return errors.New("storage and mirror must be separate, non-nested directories")
	}
	if !sameVolume(source, target) {
		return errors.New("storage and mirror must be on the same filesystem volume")
	}
	imports := ""
	if opts.Imports != "" {
		imports, err = cleanExistingDir(opts.Imports)
		if err != nil {
			return fmt.Errorf("imports: %w", err)
		}
		if imports == source || isWithin(imports, source) || isWithin(source, imports) {
			return errors.New("storage and imports must be separate, non-nested directories")
		}
		if !sameVolume(source, imports) {
			return errors.New("storage and imports must be on the same filesystem volume")
		}
	}
	statePath := opts.StateFile
	if statePath == "" {
		statePath = filepath.Join(target, ".foldermirror.json")
	}
	s := &server{source: source, target: target, imports: imports, statePath: statePath, state: State{Version: stateVersion, Files: map[string]FileRecord{}}, token: randomToken(), instance: randomToken()[:8]}
	if err := s.load(); err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/tree", s.tree)
	mux.HandleFunc("PUT /api/rules", s.rules)
	mux.HandleFunc("POST /api/plan", s.preview)
	mux.HandleFunc("POST /api/apply", s.apply)
	mux.HandleFunc("GET /api/collect/tree", s.collectTree)
	mux.HandleFunc("POST /api/collect/plan", s.collectPreview)
	mux.HandleFunc("POST /api/collect/apply", s.collectApply)
	mux.HandleFunc("GET /api/storage/tree", s.storageTree)
	mux.HandleFunc("POST /api/storage/folders", s.createStorageFolder)
	assets, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServer(http.FS(assets)))

	ln, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return err
	}
	log.Printf("FolderMirror: http://%s (storage %s, mirror %s)", ln.Addr(), source, target)
	return http.Serve(ln, securityHeaders(s.token, mux))
}

func cleanExistingDir(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	i, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !i.IsDir() {
		return "", errors.New("not a directory")
	}
	return filepath.Clean(p), nil
}

func cleanOrCreateDir(path string) (string, error) {
	p, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p, 0755); err != nil {
		return "", err
	}
	return cleanExistingDir(p)
}

func randomToken() string { b := make([]byte, 24); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func securityHeaders(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Path == "/api/status" {
			w.Header().Set("X-FolderMirror-Token", token)
		}
		if r.Method != http.MethodGet && r.Header.Get("X-FolderMirror-Token") != token {
			http.Error(w, "invalid request token", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) load() error {
	b, err := os.ReadFile(s.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(b, &s.state); err != nil {
		return fmt.Errorf("parse state: %w", err)
	}
	if s.state.Version != stateVersion {
		return fmt.Errorf("unsupported state version %d", s.state.Version)
	}
	if s.state.Files == nil {
		s.state.Files = map[string]FileRecord{}
	}
	return nil
}

func (s *server) save() error {
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.statePath + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0600); err != nil {
		return err
	}
	return replaceFile(tmp, s.statePath)
}

func (s *server) status(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, map[string]any{"storage": s.source, "mirror": s.target, "imports": s.imports, "source": s.source, "target": s.target, "platform": runtime.GOOS, "instance": s.instance, "rules": s.state.Rules, "wildcards": s.state.Wildcards, "managedFiles": len(s.state.Files)})
}

func (s *server) tree(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.buildTree()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, n)
}

func (s *server) collectTree(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.imports == "" {
		http.Error(w, "collection mode requires the -imports option", http.StatusBadRequest)
		return
	}
	n, err := buildPlainTree(s.imports)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, n)
}

func (s *server) storageTree(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tree, err := buildPlainTree(s.source)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, tree)
}

func (s *server) createStorageFolder(w http.ResponseWriter, r *http.Request) {
	var request createFolderRequest
	if err := decodeJSON(r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	parent, err := cleanRelative(request.Parent, true)
	if err != nil {
		http.Error(w, "parent: "+err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) || filepath.Base(name) != name {
		http.Error(w, "name must be one folder name without path separators", http.StatusBadRequest)
		return
	}
	parentPath, err := safePathInside(s.source, filepath.Join(s.source, parent))
	if err != nil {
		http.Error(w, "parent: "+err.Error(), http.StatusBadRequest)
		return
	}
	info, err := os.Stat(parentPath)
	if err != nil || !info.IsDir() {
		http.Error(w, "parent folder does not exist", http.StatusBadRequest)
		return
	}
	createdPath, err := safePathInside(s.source, filepath.Join(parentPath, name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.Mkdir(createdPath, 0755); err != nil {
		if errors.Is(err, os.ErrExist) {
			http.Error(w, "a file or folder with that name already exists", http.StatusConflict)
			return
		}
		writeError(w, err)
		return
	}
	tree, err := buildPlainTree(s.source)
	if err != nil {
		writeError(w, err)
		return
	}
	createdRelative, _ := filepath.Rel(s.source, createdPath)
	writeJSON(w, map[string]any{"path": slash(createdRelative), "tree": tree})
}

func (s *server) rules(w http.ResponseWriter, r *http.Request) {
	var rules []Rule
	if err := decodeJSON(r, &rules); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	clean, err := normalizeRules(rules)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Rules = clean
	if err := s.save(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"rules": clean})
}

func (s *server) preview(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, _, err := s.makePlan()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, p)
}

func (s *server) apply(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, desired, err := s.makePlan()
	if err != nil {
		writeError(w, err)
		return
	}
	result := newPlan()
	for _, a := range p.Create {
		src, dst, err := s.paths(a.Path)
		if err != nil {
			result.Skip = append(result.Skip, action{"error", a.Path, err.Error()})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err == nil {
			err = os.Link(src, dst)
		}
		if err != nil {
			result.Skip = append(result.Skip, action{"error", a.Path, err.Error()})
			continue
		}
		id, err := fileIdentity(dst)
		if err != nil {
			result.Skip = append(result.Skip, action{"error", a.Path, err.Error()})
			continue
		}
		s.state.Files[a.Path] = FileRecord{Identity: id}
		result.Create = append(result.Create, a)
	}
	for _, a := range p.Remove {
		_, dst, err := s.paths(a.Path)
		if err != nil {
			result.Skip = append(result.Skip, action{"error", a.Path, err.Error()})
			continue
		}
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			result.Skip = append(result.Skip, action{"error", a.Path, err.Error()})
			continue
		}
		delete(s.state.Files, a.Path)
		result.Remove = append(result.Remove, a)
	}
	for path := range desired {
		if rec, ok := s.state.Files[path]; ok && rec.Identity == "" {
			delete(s.state.Files, path)
		}
	}
	result.Skip = append(result.Skip, p.Skip...)
	removeEmptyDirs(s.target, s.statePath)
	if err := s.save(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (s *server) collectPreview(w http.ResponseWriter, r *http.Request) {
	var request collectRequest
	if err := decodeJSON(r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.makeCollectPlan(request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.rememberWildcard(request.Pattern); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, p)
}

func (s *server) collectApply(w http.ResponseWriter, r *http.Request) {
	var request collectRequest
	if err := decodeJSON(r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, err := s.makeCollectPlan(request)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.rememberWildcard(request.Pattern); err != nil {
		writeError(w, err)
		return
	}
	result := newPlan()
	for _, a := range p.Create {
		src, err := s.collectSourcePath(a.Detail)
		if err != nil {
			result.Skip = append(result.Skip, action{"error", a.Path, err.Error()})
			continue
		}
		dst, err := safePathInside(s.source, filepath.Join(s.source, filepath.FromSlash(a.Path)))
		if err != nil {
			result.Skip = append(result.Skip, action{"error", a.Path, err.Error()})
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err == nil {
			err = os.Link(src, dst)
		}
		if err != nil {
			result.Skip = append(result.Skip, action{"error", a.Path, err.Error()})
			continue
		}
		result.Create = append(result.Create, a)
	}
	result.Skip = append(result.Skip, p.Skip...)
	writeJSON(w, result)
}

func (s *server) buildTree() (*treeNode, error) {
	root := &treeNode{Path: "", Name: filepath.Base(s.source), Directory: true, Included: effective(s.state.Rules, "")}
	nodes := map[string]*treeNode{"": root}
	err := filepath.WalkDir(s.source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == s.source {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(s.source, path)
		rel = slash(rel)
		v, explicit := ruleValue(s.state.Rules, rel)
		n := &treeNode{Path: rel, Name: d.Name(), Directory: d.IsDir(), Included: effective(s.state.Rules, rel)}
		if explicit {
			n.Explicit = &v
		}
		parent := slash(filepath.Dir(filepath.FromSlash(rel)))
		if parent == "." {
			parent = ""
		}
		nodes[parent].Children = append(nodes[parent].Children, n)
		if d.IsDir() {
			nodes[rel] = n
		}
		return nil
	})
	return root, err
}

func buildPlainTree(rootPath string) (*treeNode, error) {
	root := &treeNode{Path: "", Name: filepath.Base(rootPath) + " (root)", Directory: true}
	nodes := map[string]*treeNode{"": root}
	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == rootPath || !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}
		rel = slash(rel)
		n := &treeNode{Path: rel, Name: d.Name(), Directory: true}
		parent := slash(filepath.Dir(filepath.FromSlash(rel)))
		if parent == "." {
			parent = ""
		}
		nodes[parent].Children = append(nodes[parent].Children, n)
		nodes[rel] = n
		return nil
	})
	return root, err
}

func (s *server) makePlan() (plan, map[string]FileRecord, error) {
	p := newPlan()
	desired := map[string]FileRecord{}
	err := filepath.WalkDir(s.source, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == s.source {
			return nil
		}
		relOS, err := filepath.Rel(s.source, path)
		if err != nil {
			return err
		}
		rel := slash(relOS)
		if d.IsDir() {
			if d.Type()&os.ModeSymlink != 0 {
				p.Skip = append(p.Skip, action{"skip", rel, "symbolic-link directory"})
				return filepath.SkipDir
			}
			return nil
		}
		if !effective(s.state.Rules, rel) {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			p.Skip = append(p.Skip, action{"skip", rel, "not a regular file"})
			return nil
		}
		src, dst, err := s.paths(rel)
		if err != nil {
			return err
		}
		si, err := os.Stat(src)
		if err != nil {
			return err
		}
		id, err := fileIdentity(src)
		if err != nil {
			return err
		}
		desired[rel] = FileRecord{Identity: id}
		di, err := os.Stat(dst)
		if errors.Is(err, os.ErrNotExist) {
			p.Create = append(p.Create, action{"link", rel, ""})
			return nil
		}
		if err != nil {
			p.Skip = append(p.Skip, action{"conflict", rel, err.Error()})
			return nil
		}
		if os.SameFile(si, di) {
			s.state.Files[rel] = FileRecord{Identity: id}
			return nil
		}
		p.Skip = append(p.Skip, action{"conflict", rel, "destination exists and is not the source file"})
		return nil
	})
	if err != nil {
		return p, desired, err
	}
	for rel, rec := range s.state.Files {
		if _, ok := desired[rel]; ok {
			continue
		}
		_, dst, err := s.paths(rel)
		if err != nil {
			p.Skip = append(p.Skip, action{"error", rel, err.Error()})
			continue
		}
		id, err := fileIdentity(dst)
		if errors.Is(err, os.ErrNotExist) {
			delete(s.state.Files, rel)
			continue
		}
		if err != nil || id != rec.Identity {
			p.Skip = append(p.Skip, action{"stale", rel, "not removed because identity changed"})
			continue
		}
		p.Remove = append(p.Remove, action{"unlink", rel, "no longer selected or present"})
	}
	sortActions := func(a []action) { sort.Slice(a, func(i, j int) bool { return a[i].Path < a[j].Path }) }
	sortActions(p.Create)
	sortActions(p.Remove)
	sortActions(p.Skip)
	return p, desired, nil
}

func (s *server) paths(rel string) (string, string, error) {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", "", errors.New("invalid relative path")
	}
	return filepath.Join(s.source, clean), filepath.Join(s.target, clean), nil
}

func (s *server) makeCollectPlan(request collectRequest) (plan, error) {
	p := newPlan()
	if s.imports == "" {
		return p, errors.New("collection mode requires the -imports option")
	}
	folder, err := cleanRelative(request.Folder, true)
	if err != nil {
		return p, fmt.Errorf("folder: %w", err)
	}
	destination, err := cleanRelative(request.Destination, false)
	if err != nil {
		return p, fmt.Errorf("storage subfolder: %w", err)
	}
	pattern := strings.TrimSpace(request.Pattern)
	if pattern == "" || strings.ContainsAny(pattern, `/\\`) {
		return p, errors.New("pattern must be a filename wildcard without path separators")
	}
	if _, err := matchWildcard(pattern, "filename"); err != nil {
		return p, fmt.Errorf("pattern: %w", err)
	}
	selected, err := cleanExistingDir(filepath.Join(s.imports, folder))
	if err != nil {
		return p, fmt.Errorf("selected import folder: %w", err)
	}
	if selected != s.imports && !isWithin(s.imports, selected) {
		return p, errors.New("selected folder resolves outside the imports root")
	}
	destinationBase, err := safePathInside(s.source, filepath.Join(s.source, destination))
	if err != nil {
		return p, fmt.Errorf("storage subfolder: %w", err)
	}
	err = filepath.WalkDir(selected, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == selected || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(selected, path)
		if err != nil {
			return err
		}
		importRel, err := filepath.Rel(s.imports, path)
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			p.Skip = append(p.Skip, action{"skip", slash(importRel), "not a regular file"})
			return nil
		}
		matched, err := matchWildcard(pattern, d.Name())
		if err != nil {
			return err
		}
		if !matched {
			return nil
		}
		dst, err := safePathInside(s.source, filepath.Join(destinationBase, rel))
		if err != nil {
			p.Skip = append(p.Skip, action{"conflict", slash(filepath.Join(destination, rel)), err.Error()})
			return nil
		}
		sourceInfo, err := os.Stat(path)
		if err != nil {
			return err
		}
		destinationInfo, err := os.Stat(dst)
		if errors.Is(err, os.ErrNotExist) {
			p.Create = append(p.Create, action{"link", slash(filepath.Join(destination, rel)), slash(importRel)})
			return nil
		}
		if err != nil {
			p.Skip = append(p.Skip, action{"conflict", slash(filepath.Join(destination, rel)), err.Error()})
			return nil
		}
		if !os.SameFile(sourceInfo, destinationInfo) {
			p.Skip = append(p.Skip, action{"conflict", slash(filepath.Join(destination, rel)), "destination exists and is not the import file"})
		}
		return nil
	})
	if err != nil {
		return p, err
	}
	sort.Slice(p.Create, func(i, j int) bool { return p.Create[i].Path < p.Create[j].Path })
	sort.Slice(p.Skip, func(i, j int) bool { return p.Skip[i].Path < p.Skip[j].Path })
	return p, nil
}

func (s *server) collectSourcePath(relative string) (string, error) {
	clean, err := cleanRelative(relative, false)
	if err != nil {
		return "", err
	}
	path := filepath.Join(s.imports, clean)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", errors.New("import source is no longer a regular file")
	}
	return safePathInside(s.imports, path)
}

func (s *server) rememberWildcard(pattern string) error {
	pattern = strings.TrimSpace(pattern)
	wildcards := []string{pattern}
	for _, existing := range s.state.Wildcards {
		if existing != pattern {
			wildcards = append(wildcards, existing)
		}
		if len(wildcards) == 12 {
			break
		}
	}
	s.state.Wildcards = wildcards
	return s.save()
}

func matchWildcard(pattern, name string) (bool, error) {
	return filepath.Match(strings.ToLower(pattern), strings.ToLower(name))
}

func cleanRelative(path string, allowEmpty bool) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
	if clean == "." {
		if allowEmpty {
			return "", nil
		}
		return "", errors.New("a subfolder is required")
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("invalid relative path")
	}
	return clean, nil
}

func safePathInside(root, candidate string) (string, error) {
	candidate = filepath.Clean(candidate)
	if candidate != root && !isWithin(root, candidate) {
		return "", errors.New("path escapes its configured root")
	}
	probe := candidate
	for {
		_, err := os.Lstat(probe)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", errors.New("no existing parent directory")
		}
		probe = parent
	}
	resolved, err := filepath.EvalSymlinks(probe)
	if err != nil {
		return "", err
	}
	if resolved != root && !isWithin(root, resolved) {
		return "", errors.New("path resolves outside its configured root")
	}
	return candidate, nil
}

func normalizeRules(in []Rule) ([]Rule, error) {
	m := map[string]bool{}
	for _, r := range in {
		p := slash(filepath.Clean(filepath.FromSlash(r.Path)))
		if p == "." {
			p = ""
		}
		if filepath.IsAbs(filepath.FromSlash(p)) || p == ".." || strings.HasPrefix(p, "../") {
			return nil, errors.New("invalid rule path")
		}
		m[p] = r.Include
	}
	out := make([]Rule, 0, len(m))
	for p, v := range m {
		out = append(out, Rule{p, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func effective(rules []Rule, path string) bool {
	best, depth := false, -1
	for _, r := range rules {
		if r.Path == "" || path == r.Path || strings.HasPrefix(path, r.Path+"/") {
			d := strings.Count(r.Path, "/") + 1
			if r.Path == "" {
				d = 0
			}
			if d >= depth {
				best, depth = r.Include, d
			}
		}
	}
	return best
}
func ruleValue(rules []Rule, path string) (bool, bool) {
	for _, r := range rules {
		if r.Path == path {
			return r.Include, true
		}
	}
	return false, false
}
func slash(p string) string { return filepath.ToSlash(p) }
func isWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func removeEmptyDirs(root, statePath string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = os.Remove(d)
	}
}

func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
