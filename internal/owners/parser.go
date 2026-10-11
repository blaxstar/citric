package owners

import (
	"bufio"
	"io"
	"strings"
)

type File struct {
	NoParent      bool          `json:"no_parent"`
	Owners        []string      `json:"owners"`    // required approvers for all files
	Reviewers     []string      `json:"reviewers"` // auto-assigned, non-blocking
	PerFile       []PerFileRule `json:"per_file"`
	RequiredLGTMs int           `json:"required_lgtms"`
}

type PerFileRule struct {
	Glob   string   `json:"glob"`
	Owners []string `json:"owners"`
}

func Parse(r io.Reader) (*File, error) {
	f := &File{RequiredLGTMs: 1}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		switch {
		case line == "set noparent":
			f.NoParent = true

		case strings.HasPrefix(line, "per-file "):
			rule := parsePerFile(strings.TrimPrefix(line, "per-file "))
			if rule != nil {
				f.PerFile = append(f.PerFile, *rule)
			}

		case strings.HasPrefix(line, "required_lgtms:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "required_lgtms:"))
			n := 0
			for _, c := range val {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			if n > 0 {
				f.RequiredLGTMs = n
			}

		case strings.HasSuffix(line, ":"):
			// section header — handled by the next lines being indented entries
			// (handled implicitly: any @user line outside a section goes to owners)

		case strings.HasPrefix(line, "@"):
			f.Owners = append(f.Owners, strings.TrimPrefix(line, "@"))
		}
	}
	return f, scanner.Err()
}

func parsePerFile(s string) *PerFileRule {
	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return nil
	}
	rule := &PerFileRule{Glob: strings.TrimSpace(parts[0])}
	for _, o := range strings.Split(parts[1], ",") {
		o = strings.TrimSpace(o)
		o = strings.TrimPrefix(o, "@")
		if o != "" {
			rule.Owners = append(rule.Owners, o)
		}
	}
	return rule
}
