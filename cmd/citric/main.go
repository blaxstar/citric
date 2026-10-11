package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/brand"
)

var cfgPath string

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func versionLine() string {
	return fmt.Sprintf("%s %s (%s/%s) · © BLAXSTAR LLC · Apache-2.0", brand.Name, version, runtime.GOOS, runtime.GOARCH)
}

var cmdVersion = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(versionLine())
	},
}

var root = &cobra.Command{
	Use:   brand.Invoked(),
	Short: "citric — self-hosted code review and CI",
	Long:  "citric is a self-hosted code review and CI platform.\nRun 'citric serve' to start the server, or use client commands to interact with a running instance.",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Make the session token available to git for restricted-repo access.
		setupGitAuth()
	},
}

func main() {
	// Under the Windows service manager, run as a service; everywhere else
	// (and for a normal console launch) this is a no-op.
	if handled, err := runAsPlatformService(brand.Name, root.Execute); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	root.PersistentFlags().StringVar(&cfgPath, "config", defaultServerConfigPath(), "server config path (used by 'citric serve')")

	// server
	root.AddCommand(cmdServe)
	root.AddCommand(cmdAdmin)
	root.AddCommand(cmdHealthcheck)
	root.AddCommand(cmdBackup)
	root.AddCommand(cmdService)
	root.AddCommand(cmdNode)
	root.AddCommand(cmdVersion)
	root.Version = version
	root.SetVersionTemplate("{{.Name}} " + version + " · © BLAXSTAR LLC · Apache-2.0\n")
	root.AddCommand(cmdRepo)

	// client
	initClientFlags()
	root.AddCommand(cmdAuth)
	root.AddCommand(cmdInit)
	root.AddCommand(cmdClone)
	root.AddCommand(cmdPull)
	root.AddCommand(cmdStatus)
	root.AddCommand(cmdAdd)
	root.AddCommand(cmdCommit)
	root.AddCommand(cmdImport)
	root.AddCommand(cmdPush)
	root.AddCommand(cmdUpdate)
	root.AddCommand(cmdPublish)
	root.AddCommand(cmdReparent)
	root.AddCommand(cmdRebase)
	root.AddCommand(cmdList)
	root.AddCommand(cmdShow)
	root.AddCommand(cmdDiff)
	root.AddCommand(cmdReview)
	root.AddCommand(cmdComment)
	root.AddCommand(cmdSubmit)
	root.AddCommand(cmdAbandon)
	root.AddCommand(cmdBuild)
	root.AddCommand(cmdSwitch)
	root.AddCommand(cmdTemplate)
	root.AddCommand(cmdRewind)

	cmdRemote.AddCommand(cmdRemoteList)
	cmdRemote.AddCommand(cmdRemoteSet)
	cmdRemote.AddCommand(cmdRemotePublish)
	cmdRemote.AddCommand(cmdRemoteRemove)
	root.AddCommand(cmdRemote)

	cmdRewind.Flags().IntP("limit", "n", 20, "how many reflog entries to list")
	cmdRewind.Flags().Bool("force", false, "reset even with a dirty working tree (discards uncommitted changes)")
	cmdRewind.Flags().BoolP("yes", "y", false, "skip the confirmation prompt")

	cmdRemoteSet.Flags().String("name", "origin", "git remote name to use for the citric server")
	cmdRemoteSet.Flags().String("repo", "", "repo name on the server (defaults to the directory name)")
	cmdRemotePublish.Flags().String("name", "public", "git remote name for the external mirror")
	cmdRemotePublish.Flags().String("branch", "", "branch to publish to (writes public_branch)")
}

// defaultServerConfigPath is /etc/citric/citric.toml, or the pre-rename
// /etc/crit/crit.toml when only that one exists.
func defaultServerConfigPath() string {
	const path = "/etc/citric/citric.toml"
	if _, err := os.Stat(path); err != nil {
		if _, err := os.Stat("/etc/crit/crit.toml"); err == nil {
			return "/etc/crit/crit.toml"
		}
	}
	return path
}
