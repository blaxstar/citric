package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	gitpkg "github.com/blaxstar/citric/internal/git"
	"github.com/blaxstar/citric/internal/model"
	"github.com/go-chi/chi/v5"
)

// gitExec builds a git command that runs against a bare repo with the correct env.
func gitExec(dataDir, repoPath string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_DIR="+repoPath)
	cmd.Env = append(cmd.Env, gitpkg.Env(dataDir)...)
	return cmd
}

// langFromPath maps a file path to a display language name.
// Returns "" for unrecognised files so they are omitted from language stats.
func langFromPath(path string) string {
	base := filepath.Base(path)
	switch strings.ToLower(base) {
	case "dockerfile":
		return "Dockerfile"
	case "makefile", "gnumakefile":
		return "Makefile"
	case "justfile":
		return "Just"
	}
	i := strings.LastIndex(base, ".")
	if i < 0 {
		return ""
	}
	switch strings.ToLower(base[i+1:]) {
	case "go":
		return "Go"
	case "ts", "tsx":
		return "TypeScript"
	case "js", "jsx", "mjs", "cjs":
		return "JavaScript"
	case "py":
		return "Python"
	case "rs":
		return "Rust"
	case "sh", "bash", "zsh", "fish":
		return "Shell"
	case "html", "htm":
		return "HTML"
	case "css":
		return "CSS"
	case "scss":
		return "SCSS"
	case "sass":
		return "Sass"
	case "json", "jsonc":
		return "JSON"
	case "toml":
		return "TOML"
	case "yaml", "yml":
		return "YAML"
	case "sql":
		return "SQL"
	case "md", "mdx", "markdown":
		return "Markdown"
	case "c", "h":
		return "C"
	case "cpp", "cc", "cxx", "hh", "hpp", "hxx":
		return "C++"
	case "java":
		return "Java"
	case "rb":
		return "Ruby"
	case "swift":
		return "Swift"
	case "kt", "kts":
		return "Kotlin"
	case "svelte":
		return "Svelte"
	case "vue":
		return "Vue"
	case "astro":
		return "Astro"
	case "tf", "tfvars":
		return "Terraform"
	case "nix":
		return "Nix"
	case "lua":
		return "Lua"
	case "php":
		return "PHP"
	case "cs":
		return "C#"
	case "fs", "fsx":
		return "F#"
	case "ex", "exs":
		return "Elixir"
	case "hs":
		return "Haskell"
	case "zig":
		return "Zig"
	case "dart":
		return "Dart"
	case "r":
		return "R"
	}
	return ""
}

var langColors = map[string]string{
	"Go":         "#00ADD8",
	"TypeScript": "#3178c6",
	"JavaScript": "#f1e05a",
	"Python":     "#3572A5",
	"Rust":       "#DEA584",
	"Shell":      "#89e051",
	"HTML":       "#e34c26",
	"CSS":        "#563d7c",
	"SCSS":       "#c6538c",
	"Sass":       "#a53b70",
	"JSON":       "#8bc34a",
	"TOML":       "#9c4221",
	"YAML":       "#cb171e",
	"SQL":        "#e38c00",
	"Markdown":   "#083fa1",
	"C":          "#555555",
	"C++":        "#f34b7d",
	"Java":       "#b07219",
	"Ruby":       "#701516",
	"Swift":      "#F05138",
	"Kotlin":     "#A97BFF",
	"Svelte":     "#ff3e00",
	"Vue":        "#41b883",
	"Astro":      "#ff5a03",
	"Dockerfile": "#384d54",
	"Makefile":   "#427819",
	"Terraform":  "#7b42bc",
	"Nix":        "#7e7eff",
	"Lua":        "#000080",
	"PHP":        "#4F5D95",
	"C#":         "#178600",
	"F#":         "#b845fc",
	"Elixir":     "#6e4a7e",
	"Haskell":    "#5e5086",
	"Zig":        "#ec915c",
	"Dart":       "#00B4AB",
	"R":          "#198ce7",
}

// handleRepoBranches returns the branch list and default branch for a repo.
// GET /repo/{name}/branches
func (s *Server) handleRepoBranches(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")

	branches, defaultBranch, err := gitpkg.ListBranches(s.cfg.Server.DataDir, repoPath)
	if err != nil {
		http.Error(w, "failed to list branches: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"branches": branches,
		"default":  defaultBranch,
	})
}

// handleRepoCommits returns recent commits on a ref.
// GET /repo/{name}/commits?ref=<branch>&n=<count>
func (s *Server) handleRepoCommits(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = "HEAD"
	}
	n := 20
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if v, err := strconv.Atoi(nStr); err == nil && v > 0 && v <= 100 {
			n = v
		}
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	commits, err := gitpkg.ListCommits(s.cfg.Server.DataDir, repoPath, ref, n)
	if err != nil {
		http.Error(w, "failed to list commits: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Resolve git author name → citric username via email where possible.
	for i, c := range commits {
		if u, err := s.db.GetUserByEmail(c.Email); err == nil && u != nil {
			commits[i].Author = u.Username
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(commits)
}

// handleRepoTree returns a directory listing.
// GET /repo/{name}/tree?ref=<branch>&path=<dir>
func (s *Server) handleRepoTree(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ref := r.URL.Query().Get("ref")
	path := r.URL.Query().Get("path")

	if ref == "" {
		_, ref, _ = gitpkg.ListBranches(s.cfg.Server.DataDir,
			filepath.Join(s.cfg.Server.ReposDir, name+".git"))
		if ref == "" {
			ref = "HEAD"
		}
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	entries, err := gitpkg.ListTree(s.cfg.Server.DataDir, repoPath, ref, path)
	if err != nil {
		// Empty repo or unknown ref
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "ref": ref, "path": path, "empty": true})
		return
	}

	// Directories first, then files — alphabetical within each group
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "tree"
		}
		return entries[i].Name < entries[j].Name
	})

	if len(entries) <= 50 {
		gitpkg.FillSizes(s.cfg.Server.DataDir, repoPath, entries)
		gitpkg.FillModifiedAt(s.cfg.Server.DataDir, repoPath, ref, path, entries)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"ref":     ref,
		"path":    path,
	})
}

// handleRepoFile returns the content of a single file.
// GET /repo/{name}/file?ref=<branch>&path=<filepath>
func (s *Server) handleRepoFile(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ref := r.URL.Query().Get("ref")
	path := r.URL.Query().Get("path")

	if ref == "" || path == "" {
		http.Error(w, "ref and path are required", http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	content, isBinary, err := gitpkg.ReadFile(s.cfg.Server.DataDir, repoPath, ref, path)
	if err != nil {
		http.Error(w, "not found: "+err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"content": content,
		"binary":  isBinary,
		"size":    len(content),
		"path":    path,
		"ref":     ref,
	})
}

// handleRepoRaw serves the raw file bytes for download.
// GET /repo/{name}/raw?ref=<branch>&path=<filepath>
func (s *Server) handleRepoRaw(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ref := r.URL.Query().Get("ref")
	path := r.URL.Query().Get("path")

	if ref == "" || path == "" {
		http.Error(w, "ref and path are required", http.StatusBadRequest)
		return
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	content, _, err := gitpkg.ReadFile(s.cfg.Server.DataDir, repoPath, ref, path)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	filename := filepath.Base(path)
	ct := mime.TypeByExtension(filepath.Ext(filename))
	if ct == "" {
		ct = "application/octet-stream"
	}
	// Serve viewable types inline; force download for everything else.
	viewable := strings.HasPrefix(ct, "image/") || strings.HasPrefix(ct, "text/") || ct == "application/pdf"
	if viewable {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename=%q`, filename))
	} else {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Write([]byte(content))
}

// handleRepoEdit commits a file edit and opens a new CL (patchset).
// POST /repo/{name}/edit
// Body: { ref, path, content, message }
func (s *Server) handleRepoEdit(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var body struct {
		Ref     string `json:"ref"`
		Path    string `json:"path"`
		Content string `json:"content"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Ref == "" || body.Path == "" || body.Message == "" {
		http.Error(w, "ref, path, content, and message are required", http.StatusBadRequest)
		return
	}

	username := s.currentUser(r)
	email := username + "@citric.local"
	if u, _ := s.db.GetUser(username); u != nil && u.Email != "" {
		email = u.Email
	}

	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")

	// Get the current tip of the target branch — this becomes base_sha.
	baseSHA, err := gitpkg.RefSHA(s.cfg.Server.DataDir, repoPath, body.Ref)
	if err != nil {
		http.Error(w, "cannot resolve ref "+body.Ref+": "+err.Error(), http.StatusBadRequest)
		return
	}

	// Generate a Change-Id and append it to the message.
	changeID, err := generateChangeID()
	if err != nil {
		http.Error(w, "failed to generate change-id", http.StatusInternalServerError)
		return
	}
	commitMsg := strings.TrimRight(body.Message, "\n") + "\n\nChange-Id: " + changeID + "\n"

	// Create a temp branch so CommitFile has a parent to read.
	tmpBranch := "_webedit_" + changeID[1:9]
	tmpRef := "refs/heads/" + tmpBranch
	if err := gitpkg.SetRef(s.cfg.Server.DataDir, repoPath, tmpRef, baseSHA); err != nil {
		http.Error(w, "failed to set temp ref: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer gitpkg.DeleteRef(s.cfg.Server.DataDir, repoPath, tmpRef) //nolint:errcheck

	if err := gitpkg.CommitFile(s.cfg.Server.DataDir, repoPath, tmpBranch,
		body.Path, body.Content, username, email, commitMsg); err != nil {
		http.Error(w, "commit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	commitSHA, err := gitpkg.RefSHA(s.cfg.Server.DataDir, repoPath, tmpRef)
	if err != nil {
		http.Error(w, "failed to read commit sha: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Create CL + patchset.
	clID, err := s.db.CreateCL(&model.CL{
		ChangeID:     changeID,
		Repo:         name,
		TargetBranch: body.Ref,
		Author:       username,
		Title:        firstLine(body.Message),
		Description:  body.Message,
	})
	if err != nil {
		http.Error(w, "create CL: "+err.Error(), http.StatusInternalServerError)
		return
	}

	repo, err := gitpkg.Open(repoPath)
	if err != nil {
		http.Error(w, "open repo: "+err.Error(), http.StatusInternalServerError)
		return
	}

	psRef := gitpkg.PatchsetRef(clID, 1)
	if err := repo.CreateRef(psRef, commitSHA); err != nil {
		http.Error(w, "create patchset ref: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := s.db.CreatePatchset(&model.Patchset{
		CLID:      clID,
		Number:    1,
		CommitSHA: commitSHA,
		BaseSHA:   baseSHA,
		Ref:       psRef,
	}); err != nil {
		http.Error(w, "create patchset: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"cl_id": clID})
}

func generateChangeID() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "I" + hex.EncodeToString(b), nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// handleRepoReadme finds and returns the README content for a repo.
// GET /repo/{name}/readme?ref=<branch>
func (s *Server) handleRepoReadme(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = "HEAD"
	}
	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	for _, candidate := range []string{"README.md", "readme.md", "README.MD", "Readme.md", "README"} {
		content, binary, err := gitpkg.ReadFile(s.cfg.Server.DataDir, repoPath, ref, candidate)
		if err != nil || binary {
			continue
		}
		writeJSON(w, map[string]any{"content": content, "path": candidate})
		return
	}
	http.Error(w, "no readme", http.StatusNotFound)
}

// handleRepoLanguages returns language breakdown (by byte count) for the repo tree.
// GET /repo/{name}/languages?ref=<branch>
func (s *Server) handleRepoLanguages(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = "HEAD"
	}
	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")

	out, err := gitExec(s.cfg.Server.DataDir, repoPath, "ls-tree", "-r", "--long", ref).Output()
	if err != nil {
		writeJSON(w, []any{})
		return
	}

	langBytes := make(map[string]int64)
	for _, line := range strings.Split(string(out), "\n") {
		tab := strings.Index(line, "\t")
		if tab < 0 {
			continue
		}
		filePath := line[tab+1:]
		meta := strings.Fields(line[:tab])
		if len(meta) < 4 || meta[1] != "blob" {
			continue
		}
		size, _ := strconv.ParseInt(meta[3], 10, 64)
		lang := langFromPath(filePath)
		if lang == "" {
			continue
		}
		langBytes[lang] += size
	}

	var total int64
	for _, b := range langBytes {
		total += b
	}
	if total == 0 {
		writeJSON(w, []any{})
		return
	}

	type langStat struct {
		Language   string  `json:"language"`
		Color      string  `json:"color"`
		Bytes      int64   `json:"bytes"`
		Percentage float64 `json:"percentage"`
	}
	result := make([]langStat, 0, len(langBytes))
	for lang, b := range langBytes {
		color := langColors[lang]
		if color == "" {
			color = "#6e7681"
		}
		result = append(result, langStat{
			Language:   lang,
			Color:      color,
			Bytes:      b,
			Percentage: float64(b) / float64(total) * 100,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Bytes > result[j].Bytes })
	writeJSON(w, result)
}

// handleRepoContributors returns a sorted list of committers by commit count.
// GET /repo/{name}/contributors
func (s *Server) handleRepoContributors(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")

	// --use-mailmap with %aE/%aN (capitalised) collapses a person's alternate
	// addresses into their canonical identity before we ever see them. Without
	// it, one human committing from two addresses lands as two contributors.
	out, err := gitExec(s.cfg.Server.DataDir, repoPath,
		"log", "--no-merges", "--use-mailmap", "--format=%aE\t%aN", "HEAD").Output()
	if err != nil {
		writeJSON(w, []any{})
		return
	}

	type entry struct {
		name  string
		count int
	}
	// Keyed by resolved identity, NOT by email. Two addresses belonging to one
	// citric account resolve to the same username, and keying on email would
	// still list them separately — twice under the very same display name.
	byIdentity := map[string]*entry{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		email, gitName := parts[0], parts[1]

		// Prefer the citric username so the row matches the rest of the UI; fall
		// back to the (mailmap-resolved) git author name.
		key := strings.ToLower(email)
		display := gitName
		if u, err := s.db.GetUserByEmail(email); err == nil && u != nil {
			key = "user:" + u.Username
			display = u.Username
		}

		if e, ok := byIdentity[key]; ok {
			e.count++
		} else {
			byIdentity[key] = &entry{name: display, count: 1}
		}
	}

	type contributor struct {
		Name    string `json:"name"`
		Commits int    `json:"commits"`
	}
	result := make([]contributor, 0, len(byIdentity))
	for _, e := range byIdentity {
		result = append(result, contributor{Name: e.name, Commits: e.count})
	}
	// Count desc, then name asc so equal counts don't reorder between requests
	// (map iteration order is randomised).
	sort.Slice(result, func(i, j int) bool {
		if result[i].Commits != result[j].Commits {
			return result[i].Commits > result[j].Commits
		}
		return result[i].Name < result[j].Name
	})
	writeJSON(w, result)
}

// handleRepoActivity returns daily commit counts for a sparkline graph.
// GET /repo/{name}/activity?days=N (default 90)
func (s *Server) handleRepoActivity(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	days := 90
	if d := r.URL.Query().Get("days"); d != "" {
		if v, err := strconv.Atoi(d); err == nil && v > 0 && v <= 365 {
			days = v
		}
	}
	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	since := fmt.Sprintf("--since=%s", time.Now().AddDate(0, 0, -days).Format("2006-01-02"))

	out, err := gitExec(s.cfg.Server.DataDir, repoPath,
		"log", "--format=%ad", "--date=format:%Y-%m-%d", since).Output()
	if err != nil {
		writeJSON(w, []any{})
		return
	}

	counts := make(map[string]int)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			counts[line]++
		}
	}

	type dayCount struct {
		Date  string `json:"date"`
		Count int    `json:"count"`
	}
	result := make([]dayCount, 0, len(counts))
	for d, c := range counts {
		result = append(result, dayCount{Date: d, Count: c})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	writeJSON(w, result)
}

// handleRepoSize returns the on-disk size of the bare repo directory in bytes.
// GET /repo/{name}/size
func (s *Server) handleRepoSize(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")

	out, err := exec.Command("du", "--bytes", "-s", repoPath).Output()
	if err != nil {
		writeJSON(w, map[string]any{"bytes": 0})
		return
	}
	parts := strings.Fields(string(out))
	if len(parts) < 1 {
		writeJSON(w, map[string]any{"bytes": 0})
		return
	}
	b, _ := strconv.ParseInt(parts[0], 10, 64)
	writeJSON(w, map[string]any{"bytes": b})
}

// handleRepoArchive streams a git archive (zip) to the client.
// GET /repo/{name}/archive?ref=<branch>&format=zip
func (s *Server) handleRepoArchive(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ref := r.URL.Query().Get("ref")
	if ref == "" {
		ref = "HEAD"
	}
	repoPath := filepath.Join(s.cfg.Server.ReposDir, name+".git")
	filename := fmt.Sprintf("%s-%s.zip", name, ref)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))

	cmd := gitExec(s.cfg.Server.DataDir, repoPath, "archive", "--format=zip", ref)
	cmd.Stdout = w
	cmd.Run() //nolint:errcheck
}
