package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/brand"
	critgit "github.com/blaxstar/citric/internal/git"
)

// installCommitMsgHook writes citric's commit-msg hook into the repo's hooks dir
// so commits get a stable Change-Id. Resolves the hooks path via git so it works
// with worktrees and custom core.hooksPath. Returns a status line for the user.
func installCommitMsgHook(dir string, force bool) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--git-path", "hooks").Output()
	if err != nil {
		return "  skip commit-msg hook (not a git repo — run inside your repo to install it)"
	}
	hooksDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(dir, hooksDir)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return fmt.Sprintf("  warn: could not create hooks dir: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "commit-msg")
	if _, err := os.Stat(hookPath); err == nil && !force {
		return "  skip commit-msg hook (already exists; --force to overwrite)"
	}
	if err := os.WriteFile(hookPath, []byte(critgit.CommitMsgHook), 0o755); err != nil {
		return fmt.Sprintf("  warn: could not write commit-msg hook: %v", err)
	}
	return "✓ installed .git/hooks/commit-msg (auto Change-Id)"
}

// ── language detection ────────────────────────────────────────────────────────

// hasFile reports whether name exists directly inside dir.
func hasFile(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// hasExt reports whether dir contains any top-level file with ext.
func hasExt(dir, ext string) bool {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			return true
		}
	}
	return false
}

// hasExtRecursive walks up to 3 levels deep looking for files with ext.
func hasExtRecursive(dir, ext string, depth int) bool {
	if depth == 0 {
		return false
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
			continue
		}
		if e.IsDir() {
			if hasExtRecursive(filepath.Join(dir, name), ext, depth-1) {
				return true
			}
		} else if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func detect(dir string) string {
	switch {
	case hasFile(dir, "go.mod"):
		return "go"
	case hasFile(dir, "Cargo.toml"):
		return "rust"
	case hasFile(dir, "package.json"):
		if hasFile(dir, "tsconfig.json") || hasExtRecursive(dir, ".ts", 3) || hasExtRecursive(dir, ".tsx", 3) {
			return "typescript"
		}
		return "node"
	case hasFile(dir, "pyproject.toml"), hasFile(dir, "setup.py"), hasFile(dir, "setup.cfg"), hasFile(dir, "requirements.txt"):
		return "python"
	case hasFile(dir, "gradlew"):
		return "android"
	case hasExtRecursive(dir, ".ps1", 2):
		return "powershell"
	case hasExtRecursive(dir, ".as", 2):
		return "actionscript"
	// loose-source fallbacks for greenfield projects without a manifest yet
	case hasExtRecursive(dir, ".py", 3):
		return "python"
	case hasExtRecursive(dir, ".rs", 3):
		return "rust"
	case hasExtRecursive(dir, ".ts", 3), hasExtRecursive(dir, ".tsx", 3):
		return "typescript"
	case hasExtRecursive(dir, ".js", 3), hasExtRecursive(dir, ".jsx", 3):
		return "node"
	case hasExtRecursive(dir, ".go", 3):
		return "go"
	case hasExt(dir, ".sh"), hasFile(dir, "Makefile"):
		return "shell"
	}
	return ""
}

func nodePackageManager(dir string) string {
	switch {
	case hasFile(dir, "pnpm-lock.yaml"):
		return "pnpm"
	case hasFile(dir, "yarn.lock"):
		return "yarn"
	default:
		return "npm"
	}
}

// ── template tool definitions ─────────────────────────────────────────────────

type templateTool struct {
	name    string
	command string
	comment string
}

type projectDef struct {
	typeName      string
	label         string
	tools         []templateTool
	scaffoldFiles map[string]string // filename → content; only written with --scaffold
	scaffoldNotes []string          // printed after scaffold
	nodeDevDeps   map[string]string // package → semver; merged into package.json devDependencies
}

func projectDefs(dir string) map[string]projectDef {
	pm := nodePackageManager(dir)
	pmCI := pm + " ci"
	if pm == "yarn" {
		pmCI = "yarn install --frozen-lockfile"
	} else if pm == "pnpm" {
		pmCI = "pnpm install --frozen-lockfile"
	}
	// npm gets --prefer-offline to use local cache, skipping registry round-trips
	if pm == "npm" {
		pmCI = "npm ci --prefer-offline --silent"
	}

	hasSCSS := hasExtRecursive(dir, ".scss", 4) || hasExtRecursive(dir, ".css", 4)
	hasVitest := hasFile(dir, "vitest.config.ts") || hasFile(dir, "vitest.config.js") ||
		hasFile(dir, "vitest.config.mts") || hasFile(dir, "vite.config.ts") || hasFile(dir, "vite.config.js")

	// --no-install is load-bearing. Bare `npx <tool>` silently downloads whatever
	// the registry has under that name when the tool isn't a local dependency —
	// and `biome` is NOT the Biome package (that's @biomejs/biome); it resolves to
	// an unrelated abandoned package. A lint run that quietly fetches a stranger's
	// package and reports success is worse than one that fails. --no-install makes
	// a missing dependency an error instead.
	nodeLintCmd := pmCI + " && npx --no-install biome check ."
	if hasSCSS {
		nodeLintCmd += " && npx --no-install stylelint '**/*.{css,scss}' --ignore-path .gitignore"
	}

	nodeTestCmd := pmCI + " && npx --no-install vitest run"
	if !hasVitest {
		nodeTestCmd = pmCI + " && " + pm + " test"
	}

	nodeScaffold := map[string]string{"biome.json": biomeJSON}
	// @biomejs/biome — NOT "biome", which is an unrelated dotenv package at 0.3.3.
	// Pinned EXACT (no caret) to stay in lockstep with the $schema in biomeJSON;
	// a floating range desyncs from it and biome then fails on its own config.
	nodeDevDeps := map[string]string{"@biomejs/biome": "2.5.3"}
	if hasVitest {
		nodeDevDeps["vitest"] = "^4.1.10"
	}
	if hasSCSS {
		nodeScaffold[".stylelintrc.json"] = stylelintRC
		// stylelint alone is not enough: .stylelintrc.json extends this shared
		// config, and stylelint fails at load time if it isn't installed too.
		// Keep these majors in lockstep — standard-scss@17 peers stylelint ^17.
		nodeDevDeps["stylelint"] = "^17.14.0"
		nodeDevDeps["stylelint-config-standard-scss"] = "^17.0.0"
	}
	nodeNotes := []string{
		"Added the lint/test tools to package.json devDependencies.",
		"Run '" + pm + " install' and commit the updated lockfile — the build runs",
		pmCI + ", so anything missing from the lockfile will not be installed.",
		"",
		"Suggested package.json scripts:",
		`  "lint": "biome check .",`,
		`  "test": "vitest run"`,
	}

	pyprojectExists := hasFile(dir, "pyproject.toml")
	pyScaffold := map[string]string{}
	pyNotes := []string{}
	if !pyprojectExists {
		pyScaffold["pyproject.toml"] = fmt.Sprintf(pyprojectTemplate, filepath.Base(dir))
		pyNotes = append(pyNotes, "Created pyproject.toml — add your runtime dependencies under [project] dependencies.")
	} else {
		pyNotes = append(pyNotes,
			"pyproject.toml already exists. Make sure it includes:",
			"  [project.optional-dependencies]",
			`  dev = ["ruff>=0.4", "pytest>=8"]`,
			"",
			"  [tool.ruff.lint]",
			`  select = ["E", "F", "I", "UP"]`,
		)
	}

	return map[string]projectDef{
		"go": {
			typeName: "go",
			label:    "Go",
			tools: []templateTool{
				{name: "lint", command: "go vet ./... && golangci-lint run ./...", comment: "golangci-lint: https://golangci-lint.run/usage/install/"},
				{name: "test", command: "go test -race ./..."},
				{name: "integration", command: "go test -tags integration -race ./...", comment: "remove if unused"},
			},
			scaffoldNotes: []string{
				"Install golangci-lint: https://golangci-lint.run/usage/install/",
				"It runs staticcheck, errcheck, govet, unused, and more in one pass.",
			},
		},

		"rust": {
			typeName: "rust",
			label:    "Rust",
			tools: []templateTool{
				{name: "lint", command: "cargo clippy -- -D warnings"},
				{name: "test", command: "cargo test"},
				{name: "integration", command: "cargo test --test '*'", comment: "integration tests in tests/ — remove if unused"},
			},
		},

		"typescript": {
			typeName: "typescript",
			label:    "TypeScript / Node.js",
			tools: []templateTool{
				{name: "lint", command: nodeLintCmd},
				{name: "test", command: nodeTestCmd},
				{name: "integration", command: pmCI + " && " + pm + " run test:integration", comment: "remove if unused"},
			},
			scaffoldFiles: nodeScaffold,
			scaffoldNotes: nodeNotes,
			nodeDevDeps:   nodeDevDeps,
		},

		"node": {
			typeName: "node",
			label:    "Node.js",
			tools: []templateTool{
				{name: "lint", command: nodeLintCmd},
				{name: "test", command: nodeTestCmd},
			},
			scaffoldFiles: nodeScaffold,
			scaffoldNotes: nodeNotes,
			nodeDevDeps:   nodeDevDeps,
		},

		"python": {
			typeName: "python",
			label:    "Python",
			tools: []templateTool{
				{name: "lint", command: "uv sync --dev --quiet && uv run ruff check . && uv run ruff format --check ."},
				{name: "test", command: "uv run pytest", comment: "uv sync already ran in lint step"},
				{name: "integration", command: "uv run pytest tests/integration/", comment: "remove if unused"},
			},
			scaffoldFiles: pyScaffold,
			scaffoldNotes: append([]string{
				"Install uv once on each machine: curl -LsSf https://astral.sh/uv/install.sh | sh",
			}, pyNotes...),
		},

		"android": {
			typeName: "android",
			label:    "Android / Kotlin (Gradle)",
			tools: []templateTool{
				{name: "build", command: "./gradlew assembleDebug"},
				{name: "lint", command: "./gradlew lint"},
				{name: "test", command: "./gradlew testDebugUnitTest"},
				{name: "format", command: "ktlint 'app/src/main/**/*.kt'", comment: "standalone CLI, not the Gradle plugin — https://github.com/ktlint/ktlint/releases"},
			},
			scaffoldNotes: []string{
				"Needs a JDK 17+ (JAVA_HOME) and the Android SDK cmdline-tools (ANDROID_HOME)",
				"with the platform/build-tools your compileSdk needs, plus the standalone",
				"ktlint CLI: https://github.com/ktlint/ktlint/releases",
				"",
				"'integration' (./gradlew connectedDebugAndroidTest) needs a running",
				"emulator/device, which a headless build box doesn't have — left out here.",
				"Add it back once there's a device farm or emulator in the build environment.",
			},
		},

		"shell": {
			typeName: "shell",
			label:    "Shell scripts",
			tools: []templateTool{
				{name: "lint", command: "find . -name '*.sh' -not -path './vendor/*' -exec shellcheck {} +"},
				{name: "test", command: "bash tests/run.sh", comment: "adjust to your test runner"},
			},
			scaffoldNotes: []string{"Install shellcheck: apt install shellcheck  /  brew install shellcheck"},
		},

		"powershell": {
			typeName: "powershell",
			label:    "PowerShell",
			tools: []templateTool{
				{name: "lint", command: "pwsh -Command \"Get-ChildItem -Recurse -Filter *.ps1 | ForEach-Object { Invoke-ScriptAnalyzer $_.FullName -Severity Warning,Error }\""},
				{name: "test", command: "pwsh -Command \"Invoke-Pester -CI\""},
			},
			scaffoldNotes: []string{
				"Install PSScriptAnalyzer: Install-Module PSScriptAnalyzer -Force",
				"Install Pester:           Install-Module Pester -Force",
			},
		},

		"actionscript": {
			typeName: "actionscript",
			label:    "ActionScript 3",
			tools: []templateTool{
				{name: "lint", command: "mxmlc -strict=true -warnings=true src/Main.as -output /tmp/citric-as3-check.swf", comment: "adjust entry point; uses Apache Flex SDK mxmlc"},
				{name: "test", command: "bash tests/run.sh", comment: "AS3 test runner is project-specific; adjust or remove"},
			},
			scaffoldNotes: []string{
				"Install Apache Royale compiler (mxmlc replacement): https://royale.apache.org",
				"Or set FLEX_HOME and use the classic Flex SDK mxmlc.",
			},
		},

		"config": {
			typeName: "config",
			label:    "Config files (JSON / TOML / XML / YAML)",
			tools: []templateTool{
				{name: "lint", command: "find . -name '*.json' -exec jq empty {} + && find . -name '*.toml' -exec taplo lint {} + && find . -name '*.xml' -exec xmllint --noout {} + && find . -name '*.yml' -o -name '*.yaml' | xargs yamllint"},
			},
			scaffoldNotes: []string{
				"Install: apt install jq libxml2-utils  /  pip install yamllint  /  cargo install taplo-cli",
			},
		},
	}
}

// ── scaffold file templates ───────────────────────────────────────────────────

// biomeJSON — the $schema version must match the @biomejs/biome version installed
// by nodeDevDeps EXACTLY, or `biome check` emits a deserialize error and fails the
// build. That's why the devDep is pinned exact (no caret): a floating ^2.5.2 would
// resolve to 2.5.3 and desync from the schema pinned here. Bump both together.
const biomeJSON = `{
  "$schema": "https://biomejs.dev/schemas/2.5.3/schema.json",
  "linter": {
    "enabled": true
  },
  "formatter": {
    "enabled": true,
    "indentStyle": "space",
    "indentWidth": 2,
    "lineWidth": 100
  },
  "assist": {
    "actions": {
      "source": {
        "organizeImports": "on"
      }
    }
  },
  "files": {
    "includes": ["**", "!dist", "!build", "!node_modules", "!coverage", "!.next", "!**/*.min.js"]
  }
}
`

const stylelintRC = `{
  "extends": ["stylelint-config-standard-scss"],
  "rules": {
    "selector-class-pattern": null
  }
}
`

const pyprojectTemplate = `[project]
name = "%s"
requires-python = ">=3.11"
dependencies = []

[project.optional-dependencies]
dev = ["ruff>=0.4", "pytest>=8", "pytest-cov"]

[tool.ruff]
target-version = "py311"

[tool.ruff.lint]
select = ["E", "F", "I", "UP"]

[tool.pytest.ini_options]
testpaths = ["tests"]
`

// ── .gitignore templates ──────────────────────────────────────────────────────

// gitignoreCommon is appended to every scaffolded .gitignore.
const gitignoreCommon = `
# citric
/data/run/

# editors / OS
.DS_Store
*.swp
.idea/
.vscode/
*.log
`

// gitignoreByType maps a project type to a language-appropriate .gitignore body.
//
// Build/dependency directories are deliberately left UNANCHORED (`node_modules/`,
// not `/node_modules/`). A leading slash pins a pattern to the repo root, so an
// anchored `/node_modules/` misses `web/node_modules` and `server/node_modules` —
// which is every repo with more than one package.json. These directories are
// artifacts wherever they appear, so they should match at any depth.
//
// `/bin/` stays anchored on purpose: unlike the others, "bin" is a plausible name
// for a real source directory nested in a tree.
var gitignoreByType = map[string]string{
	"go": `# binaries / build
/bin/
*.exe
*.test
*.out
dist/
`,
	"rust": `# rust
target/
**/*.rs.bk
Cargo.lock
`,
	"node": `# node
node_modules/
dist/
build/
coverage/
.cache/
*.tsbuildinfo
.env
.env.*
`,
	"typescript": `# node / typescript
node_modules/
dist/
build/
coverage/
.cache/
*.tsbuildinfo
.env
.env.*
`,
	"python": `# python
__pycache__/
*.py[cod]
.venv/
venv/
.eggs/
*.egg-info/
build/
dist/
.pytest_cache/
.ruff_cache/
.coverage
htmlcov/
`,
}

// gitignoreFor returns the .gitignore body for typeName plus the common section.
func gitignoreFor(typeName string) string {
	body := gitignoreByType[typeName]
	return strings.TrimLeft(body+gitignoreCommon, "\n")
}

// addNodeDevDeps merges the lint/test tooling into package.json devDependencies.
// Uses `npm pkg set` rather than rewriting the JSON ourselves so the file's key
// order and formatting survive. Only declares the deps — the caller is told to
// run install, since that's what refreshes the lockfile the build actually uses.
func addNodeDevDeps(dir string, deps map[string]string) string {
	if !hasFile(dir, "package.json") {
		return "  skip devDependencies (no package.json here)"
	}
	names := make([]string, 0, len(deps))
	for name := range deps {
		names = append(names, name)
	}
	sort.Strings(names)

	args := []string{"pkg", "set"}
	for _, name := range names {
		args = append(args, fmt.Sprintf("devDependencies.%s=%s", name, deps[name]))
	}
	cmd := exec.Command("npm", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Sprintf("  warn: could not update package.json (%v: %s)\n"+
			"  add these to devDependencies by hand: %s",
			err, strings.TrimSpace(string(out)), strings.Join(names, ", "))
	}
	return "✓ added devDependencies: " + strings.Join(names, ", ")
}

// installGitignore writes a language-appropriate .gitignore into dir, unless one
// already exists (or force is set). Returns a status line for the user.
func installGitignore(dir, typeName string, force bool) string {
	path := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(path); err == nil && !force {
		return "  skip .gitignore (already exists; --force to overwrite)"
	}
	if err := os.WriteFile(path, []byte(gitignoreFor(typeName)), 0o644); err != nil {
		return fmt.Sprintf("  warn: could not write .gitignore: %v", err)
	}
	return "✓ wrote .gitignore"
}

// ── .citric.toml rendering ──────────────────────────────────────────────────────

func renderTemplate(def projectDef, repoName string, defs map[string]projectDef) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# citric build config — %s\n", repoName)
	fmt.Fprintf(&b, "# Project type: %s\n", def.label)
	fmt.Fprintf(&b, "#\n")
	fmt.Fprintf(&b, "# Tool names must match entries in the server's citric.toml [[build_tool]] list.\n")
	fmt.Fprintf(&b, "# Commands run in the repo root on the exact commit being reviewed.\n")
	fmt.Fprintf(&b, "# Env available: CITRIC_CL_ID, CITRIC_COMMIT_SHA, CITRIC_REPO, CITRIC_TARGET_BRANCH\n")
	fmt.Fprintf(&b, "\n")
	// These MUST stay above the [[tool]] blocks. They are top-level keys, and in
	// TOML every key after a table header belongs to that table — uncommented
	// below the tools, public_remote would silently become a field of the last
	// [[tool]] and `citric publish` would report it as unset.
	b.WriteString("# Open-source mirror. Set these and run `citric publish` to push a clean copy\n")
	b.WriteString("# of this branch (Change-Id trailers stripped) to an external git host.\n")
	b.WriteString("# Keep them above the [[tool]] blocks — a top-level key written below a\n")
	b.WriteString("# table header is parsed as part of that table, not the file.\n")
	b.WriteString("# public_remote = \"git@github.com:you/" + repoName + ".git\"\n")
	b.WriteString("# public_branch = \"main\"\n")
	b.WriteString("\n")
	for _, t := range def.tools {
		fmt.Fprintf(&b, "[[tool]]\n")
		fmt.Fprintf(&b, "name    = %q\n", t.name)
		if t.comment != "" {
			fmt.Fprintf(&b, "command = %q  # %s\n", t.command, t.comment)
		} else {
			fmt.Fprintf(&b, "command = %q\n", t.command)
		}
		fmt.Fprintf(&b, "\n")
	}
	b.WriteString("# Per-tool env vars (uncomment as needed, inside a [[tool]] block above):\n")
	b.WriteString("# [tool.env]\n")
	b.WriteString("# MY_VAR = \"value\"\n")
	b.WriteString(referenceBlock(def.typeName, defs))
	return b.String()
}

// buildToolReference documents the server-side [[build_tool]] registry. The
// server's citric.toml is authoritative — this is the stock set, listed so a repo
// author can see what is available without reading the server config.
var buildToolReference = []struct{ name, note string }{
	{"lint", "required — a CL cannot be submitted until this passes"},
	{"test", "informational; gated behind lint"},
	{"integration", "informational; gated behind test"},
	{"format", "informational; independent (a check, not a rewrite)"},
}

// referenceBlock renders the commented catalogue appended to every generated
// .citric.toml: every tool the server knows about, and the recommended commands
// for every language citric supports. Both are derived from the live definitions
// rather than restated, so this cannot drift from what the CLI actually does.
func referenceBlock(currentType string, defs map[string]projectDef) string {
	var b strings.Builder
	b.WriteString("\n# ─────────────────────────────────────────────────────────────────────────────\n")
	b.WriteString("# REFERENCE — everything citric supports. Nothing below is active; it is here so\n")
	b.WriteString("# you can add a tool or switch language without going and looking it up.\n")
	b.WriteString("#\n")
	b.WriteString("# Tools. A [[tool]] entry with no command, or no entry at all, is waived for\n")
	b.WriteString("# this repo (recorded as a pass with a [skipped] log line), so you only declare\n")
	b.WriteString("# what you actually run.\n")
	b.WriteString("#\n")
	for _, t := range buildToolReference {
		fmt.Fprintf(&b, "#   %-12s %s\n", t.name, t.note)
	}
	b.WriteString("#\n")
	b.WriteString("# Languages. Detected automatically; override with `citric template --type <name>`.\n")

	for _, name := range sortedTypeNames(defs) {
		d := defs[name]
		marker := ""
		if name == currentType {
			marker = "   ← this repo"
		}
		fmt.Fprintf(&b, "#\n#   %s (--type %s)%s\n", d.label, d.typeName, marker)
		for _, t := range d.tools {
			fmt.Fprintf(&b, "#     %-12s %s\n", t.name, t.command)
		}
	}
	return b.String()
}

// sortedTypeNames keeps the reference block in a stable, readable order rather
// than Go's randomised map iteration.
func sortedTypeNames(defs map[string]projectDef) []string {
	names := make([]string, 0, len(defs))
	for k := range defs {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// resolveDef returns the project definition for forceType, or auto-detects from
// dir, falling back to the "config" template when nothing matches.
func resolveDef(dir, forceType string) (projectDef, error) {
	defs := projectDefs(dir)
	typeName := forceType
	if typeName == "" {
		typeName = detect(dir)
	}
	if typeName == "" {
		fmt.Fprintln(os.Stderr, "warning: could not detect project type — using 'config' template")
		fmt.Fprintln(os.Stderr, "         override with --type: go, rust, typescript, node, python, android, shell, powershell, actionscript, config")
		typeName = "config"
	}
	def, ok := defs[typeName]
	if !ok {
		return projectDef{}, fmt.Errorf("unknown type %q; valid: go, rust, typescript, node, python, android, shell, powershell, actionscript, config", typeName)
	}
	return def, nil
}

// scaffoldProject writes .citric.toml for dir's project type. When withExtras is
// true it also writes supporting config files and installs the commit-msg hook.
// Existing files are skipped unless force is set. Used by both `template` and `init`.
func scaffoldProject(dir, forceType string, force, withExtras bool) error {
	def, err := resolveDef(dir, forceType)
	if err != nil {
		return err
	}

	critToml := brand.RepoConfigPath(dir)
	if _, err := os.Stat(critToml); err == nil && !force {
		fmt.Println("  skip .citric.toml (already exists; --force to overwrite)")
	} else if err := os.WriteFile(critToml, []byte(renderTemplate(def, filepath.Base(dir), projectDefs(dir))), 0o644); err != nil {
		return fmt.Errorf("write .citric.toml: %w", err)
	} else {
		fmt.Printf("✓ wrote .citric.toml  (%s)\n", def.label)
	}

	if !withExtras {
		return nil
	}

	for name, body := range def.scaffoldFiles {
		outPath := filepath.Join(dir, name)
		if _, err := os.Stat(outPath); err == nil && !force {
			fmt.Printf("  skip %s (already exists; --force to overwrite)\n", name)
			continue
		}
		if err := os.WriteFile(outPath, []byte(body), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  warn: could not write %s: %v\n", name, err)
			continue
		}
		fmt.Printf("✓ wrote %s\n", name)
	}

	// The generated lint/test commands run the tools with `npx --no-install`, so
	// they must be real dependencies or the build fails. Writing the config files
	// without the packages that read them is what produced builds that fetched an
	// arbitrary registry package named "biome" and a stylelint with no shared
	// config to extend.
	if len(def.nodeDevDeps) > 0 {
		fmt.Println(addNodeDevDeps(dir, def.nodeDevDeps))
	}

	// language-appropriate .gitignore so build/editor cruft stays out of CLs.
	fmt.Println(installGitignore(dir, def.typeName, force))

	// commit-msg hook so commits get a stable Change-Id (keeps amended re-pushes
	// on the same CL instead of spawning new ones).
	fmt.Println(installCommitMsgHook(dir, force))

	for _, n := range def.scaffoldNotes {
		if n == "" {
			fmt.Println()
		} else {
			fmt.Println(" ", n)
		}
	}
	return nil
}

// ── command ───────────────────────────────────────────────────────────────────

var cmdTemplate = &cobra.Command{
	Use:   "template",
	Short: "Generate a .citric.toml for this repo (auto-detects project type)",
	Long: `Detects the project type and prints a ready-to-use .citric.toml.

Detection order: go.mod → Cargo.toml → package.json (+tsconfig → typescript) →
pyproject.toml/setup.py/requirements.txt → gradlew → *.ps1 → *.as → *.sh/Makefile

Flags:
  --write     write .citric.toml to disk (default: print to stdout)
  --scaffold  write .citric.toml AND create supporting config files
              (biome.json, .stylelintrc.json, pyproject.toml as needed)
  --type      override detection: go, rust, typescript, node, python, android,
              shell, powershell, actionscript, config
  --force     overwrite existing files`,

	RunE: func(cmd *cobra.Command, args []string) error {
		doWrite, _ := cmd.Flags().GetBool("write")
		doScaffold, _ := cmd.Flags().GetBool("scaffold")
		forceType, _ := cmd.Flags().GetString("type")
		force, _ := cmd.Flags().GetBool("force")

		dir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get cwd: %w", err)
		}

		// print-only mode (no --write/--scaffold)
		if !doWrite && !doScaffold {
			def, err := resolveDef(dir, forceType)
			if err != nil {
				return err
			}
			fmt.Print(renderTemplate(def, filepath.Base(dir), projectDefs(dir)))
			fmt.Fprintf(os.Stderr, "\n# detected: %s\n", def.label)
			fmt.Fprintln(os.Stderr, "# run with --write to save to .citric.toml, --scaffold to also create supporting config files")
			return nil
		}

		if err := scaffoldProject(dir, forceType, force, doScaffold); err != nil {
			return err
		}
		fmt.Println()
		fmt.Println("next: commit .citric.toml (and any scaffolded files), then push a CL.")
		return nil
	},
}

func init() {
	cmdTemplate.Flags().Bool("write", false, "write to .citric.toml instead of printing to stdout")
	cmdTemplate.Flags().Bool("scaffold", false, "write .citric.toml and create supporting config files (biome.json, .stylelintrc.json, pyproject.toml)")
	cmdTemplate.Flags().Bool("force", false, "overwrite existing files")
	cmdTemplate.Flags().String("type", "", "override project type detection")
}
