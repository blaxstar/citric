package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/blaxstar/citric/internal/brand"
)

// serviceSpec describes the background service to install: which citric
// subcommand it runs and under what name.
type serviceSpec struct {
	Name        string   // service / unit name
	DisplayName string   // human-readable name (Windows, launchd comment)
	Description string   // one line
	Args        []string // arguments after the executable, e.g. serve --config …
	User        string   // account to run as (systemd); empty = root/system
}

var cmdService = &cobra.Command{
	Use:   "service",
	Short: "Install or remove citric as a system service (systemd, launchd, Windows)",
}

var cmdServiceInstall = &cobra.Command{
	Use:   "install",
	Short: "Install and start 'citric serve' as a system service",
	Long: `Registers this binary to run 'citric serve' at boot and starts it:

  Linux    a systemd unit at /etc/systemd/system/citric.service
  macOS    a launchd daemon at /Library/LaunchDaemons/io.github.blaxstar.citric.plist
  Windows  a Windows service named "citric"

Needs root / Administrator. The service uses --config if given (made
absolute), else the default config path; CITRIC_* variables can be set in the
unit (systemd), plist (launchd) or service registry environment (Windows).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		user, _ := cmd.Flags().GetString("user")
		spec, err := serverServiceSpec(cmd, user)
		if err != nil {
			return err
		}
		return installService(spec)
	},
}

var cmdServiceUninstall = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop and remove the citric system service",
	RunE: func(cmd *cobra.Command, args []string) error {
		return uninstallService(brand.Name)
	},
}

func serverServiceSpec(cmd *cobra.Command, user string) (serviceSpec, error) {
	args := []string{"serve"}
	if cmd.Flags().Changed("config") {
		abs, err := filepath.Abs(cfgPath)
		if err != nil {
			return serviceSpec{}, err
		}
		args = append(args, "--config", abs)
	}
	return serviceSpec{
		Name:        brand.Name,
		DisplayName: "citric",
		Description: "citric code review and CI server",
		Args:        args,
		User:        user,
	}, nil
}

// executablePath is the absolute, symlink-resolved path of this binary, which
// is what the service definition must point at.
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Abs(exe)
}

func init() {
	cmdServiceInstall.Flags().String("user", brand.Name, "account the service runs as (systemd only)")
	cmdService.AddCommand(cmdServiceInstall, cmdServiceUninstall)
}
