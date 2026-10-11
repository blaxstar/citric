package owners

import (
	"os"
	"path/filepath"
	"strings"
)

type ResolvedOwners struct {
	Required      []string // union of all owners for this file path
	RequiredLGTMs int
}

// Resolve walks from filePath up to the repo root, collecting OWNERS entries.
// repoRoot is the absolute path to the repository root on disk.
func Resolve(repoRoot, filePath string) (*ResolvedOwners, error) {
	result := &ResolvedOwners{RequiredLGTMs: 1}
	seen := map[string]bool{}

	dir := filepath.Dir(filepath.Join(repoRoot, filePath))
	for {
		ownersPath := filepath.Join(dir, "OWNERS")
		f, err := os.Open(ownersPath)
		if err == nil {
			parsed, err := Parse(f)
			f.Close()
			if err != nil {
				return nil, err
			}

			owners := resolveOwnersForFile(parsed, filepath.Base(filePath))
			for _, o := range owners {
				if !seen[o] {
					seen[o] = true
					result.Required = append(result.Required, o)
				}
			}
			if parsed.RequiredLGTMs > result.RequiredLGTMs {
				result.RequiredLGTMs = parsed.RequiredLGTMs
			}
			if parsed.NoParent {
				break
			}
		}

		if dir == repoRoot || dir == "." || dir == "/" {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return result, nil
}

// resolveOwnersForFile returns the effective owners list for a specific filename,
// applying per-file glob overrides before falling back to the directory owners list.
func resolveOwnersForFile(f *File, filename string) []string {
	for _, rule := range f.PerFile {
		if matchGlob(rule.Glob, filename) {
			return rule.Owners
		}
	}
	return f.Owners
}

func matchGlob(pattern, name string) bool {
	matched, err := filepath.Match(pattern, name)
	if err != nil {
		return false
	}
	// also match on full relative path suffix
	if !matched {
		matched = strings.HasSuffix(name, strings.TrimPrefix(pattern, "*"))
	}
	return matched
}
