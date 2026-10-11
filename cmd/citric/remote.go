package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/brand"
)

// citric works with two remotes and they are configured in different places,
// which is exactly why this command exists:
//
//   the citric remote     a plain git remote (default "origin") pointing at
//                       <server>/git/<repo>.git — lives in .git/config
//   the publish remote  the external mirror, whose URL lives in the repo's
//                       .citric.toml as public_remote (so it travels with the
//                       repo); `citric publish` mirrors the git remote from it
//
// Before this, the first was only ever set as a side effect of `citric init` and
// the second had to be hand-written into .citric.toml.

var cmdRemote = &cobra.Command{
	Use:   "remote",
	Short: "Show or configure the citric and publish remotes",
	Long: `Show or configure the remotes citric uses.

  citric remote list                 list every git remote, tagging citric's
  citric remote set [url]            point the citric remote at the server
  citric remote publish <url>        set the open-source mirror in .citric.toml
  citric remote remove <name>        drop a remote

Run with no subcommand to list.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listRemotes()
	},
}

var cmdRemoteList = &cobra.Command{
	Use:   "list",
	Short: "List git remotes, marking the citric and publish ones",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listRemotes()
	},
}

var cmdRemoteSet = &cobra.Command{
	Use:   "set [url]",
	Short: "Point the citric remote at the server (derived from your auth config if omitted)",
	Long: `Set the git remote citric pushes reviews to.

With no url, derives <server>/git/<repo>.git from your client config (whatever
'citric auth setup' recorded) and the repository's directory name — the same URL
'citric init' would have wired up.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		repoName, _ := cmd.Flags().GetString("repo")

		url := ""
		if len(args) == 1 {
			url = args[0]
		} else {
			cfg, err := loadClientConfig()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.Server == "" {
				return fmt.Errorf("no server configured — run 'citric auth setup' first, or pass a url")
			}
			if repoName == "" {
				root, err := gitOut("rev-parse", "--show-toplevel")
				if err != nil {
					return fmt.Errorf("not inside a git repository: %w", err)
				}
				repoName = filepath.Base(root)
			}
			url = strings.TrimRight(cfg.Server, "/") + "/git/" + strings.TrimSuffix(repoName, ".git") + ".git"
		}

		if err := ensureRemote(name, url); err != nil {
			return err
		}
		fmt.Printf("✓ remote %q → %s\n", name, url)
		return nil
	},
}

var cmdRemotePublish = &cobra.Command{
	Use:   "publish <url>",
	Short: "Set the open-source mirror remote (writes public_remote to .citric.toml)",
	Long: `Set the external mirror that 'citric publish' pushes to.

The URL is written to the repo's .citric.toml as public_remote, not to .git/config,
so it is shared with everyone who clones the repo. The matching git remote is
wired up too, so 'citric publish' has something to push to immediately.

Commit the .citric.toml change to make it stick for everyone else.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		branch, _ := cmd.Flags().GetString("branch")
		remoteName, _ := cmd.Flags().GetString("name")

		root, err := gitOut("rev-parse", "--show-toplevel")
		if err != nil {
			return fmt.Errorf("not inside a git repository: %w", err)
		}
		path := brand.RepoConfigPath(root)

		if err := setRepoTomlKey(path, "public_remote", url); err != nil {
			return err
		}
		fmt.Printf("✓ public_remote → %s\n", url)
		if branch != "" {
			if err := setRepoTomlKey(path, "public_branch", branch); err != nil {
				return err
			}
			fmt.Printf("✓ public_branch → %s\n", branch)
		}
		if err := ensureRemote(remoteName, url); err != nil {
			return err
		}
		fmt.Printf("✓ remote %q → %s\n", remoteName, url)
		fmt.Println("\nnext: commit .citric.toml, then 'citric publish'")
		return nil
	},
}

var cmdRemoteRemove = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm"},
	Short:   "Remove a git remote",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if _, err := gitOut("remote", "get-url", name); err != nil {
			return fmt.Errorf("no remote named %q", name)
		}
		if out, err := exec.Command("git", "remote", "remove", name).CombinedOutput(); err != nil {
			return fmt.Errorf("git remote remove %s: %w\n%s", name, err, out)
		}
		fmt.Printf("✓ removed remote %q\n", name)

		// The publish URL lives in .citric.toml, so dropping the git remote alone
		// would leave a stale mirror target behind. Say so rather than guessing —
		// .citric.toml is shared, and clearing it affects everyone.
		if cfg, err := loadRepoPublishConfig(); err == nil && cfg.PublicRemote != "" {
			if cur, _ := gitOut("config", "--get", "remote."+name+".url"); cur == "" {
				fmt.Printf("\nnote: .citric.toml still has public_remote = %q\n", cfg.PublicRemote)
				fmt.Println("      clear it by hand if this repo is no longer mirrored.")
			}
		}
		return nil
	},
}

func listRemotes() error {
	out, err := gitOut("remote", "-v")
	if err != nil {
		return fmt.Errorf("not inside a git repository: %w", err)
	}

	// git remote -v prints fetch and push lines per remote; collapse to name→url.
	urls := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			urls[f[0]] = f[1]
		}
	}

	cfg, _ := loadClientConfig()
	server := ""
	if cfg != nil {
		server = strings.TrimRight(cfg.Server, "/")
	}
	pub, _ := loadRepoPublishConfig()
	publicRemote := ""
	if pub != nil {
		publicRemote = pub.PublicRemote
	}

	if len(urls) == 0 {
		fmt.Println("No git remotes configured.")
	} else {
		names := make([]string, 0, len(urls))
		for n := range urls {
			names = append(names, n)
		}
		sort.Strings(names)

		fmt.Println("Remotes:")
		fmt.Println()
		for _, n := range names {
			tag := ""
			switch {
			case server != "" && strings.HasPrefix(urls[n], server):
				tag = "  [citric]"
			case publicRemote != "" && urls[n] == publicRemote:
				tag = "  [publish]"
			}
			fmt.Printf("  %-10s %s%s\n", n, urls[n], tag)
		}
	}

	fmt.Println()
	if publicRemote != "" {
		fmt.Printf("publish mirror (.citric.toml): %s\n", publicRemote)
		if pub.PublicBranch != "" {
			fmt.Printf("publish branch:              %s\n", pub.PublicBranch)
		}
	} else {
		fmt.Println("publish mirror (.citric.toml): not set — 'citric remote publish <url>'")
	}
	return nil
}

// setRepoTomlKey sets a top-level key in .citric.toml, replacing an existing
// assignment (including the commented-out hint 'citric template' writes) in place
// so surrounding comments and [[tool]] blocks survive untouched. A full decode
// and re-encode would drop every comment in the file.
func setRepoTomlKey(path, key, value string) error {
	line := fmt.Sprintf("%s = %q", key, value)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(path, []byte(line+"\n"), 0o644)
		}
		return err
	}

	// Matches "key = ..." and the template's commented "# key = ..." hint.
	re := regexp.MustCompile(`^[ \t]*#?[ \t]*` + regexp.QuoteMeta(key) + `[ \t]*=`)
	text := strings.TrimRight(string(data), "\n")
	lines := strings.Split(text, "\n")

	// A top-level key must appear before the first table header: in TOML every
	// key after "[table]" belongs to that table. Writing public_remote below the
	// [[tool]] blocks makes it a field of the last tool, where citric publish will
	// never see it — the file still parses, so the mistake is silent.
	firstTable := func() int {
		for i, l := range lines {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "[") {
				return i
			}
		}
		return -1
	}

	existing := -1
	for i, l := range lines {
		if re.MatchString(l) {
			existing = i
			break
		}
	}

	switch ft := firstTable(); {
	case existing >= 0 && (ft == -1 || existing < ft):
		// Already in a valid spot — replace in place and keep its comment context.
		lines[existing] = line
	default:
		if existing >= 0 {
			// Present, but stranded inside a table. Pull it out.
			lines = append(lines[:existing], lines[existing+1:]...)
		}
		if ft = firstTable(); ft == -1 {
			lines = append(lines, line)
		} else {
			lines = append(lines[:ft], append([]string{line, ""}, lines[ft:]...)...)
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
