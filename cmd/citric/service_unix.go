//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const launchdLabelPrefix = "io.github.blaxstar."

func installService(spec serviceSpec) error {
	exe, err := executablePath()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "linux":
		return installSystemd(spec, exe)
	case "darwin":
		return installLaunchd(spec, exe)
	}
	return fmt.Errorf("service install is not supported on %s", runtime.GOOS)
}

func uninstallService(name string) error {
	switch runtime.GOOS {
	case "linux":
		unit := "/etc/systemd/system/" + name + ".service"
		_ = exec.Command("systemctl", "disable", "--now", name).Run()
		if err := os.Remove(unit); err != nil && !os.IsNotExist(err) {
			return err
		}
		_ = exec.Command("systemctl", "daemon-reload").Run()
		fmt.Printf("✓ removed %s\n", unit)
		return nil
	case "darwin":
		plist := "/Library/LaunchDaemons/" + launchdLabelPrefix + name + ".plist"
		_ = exec.Command("launchctl", "bootout", "system", plist).Run()
		if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Printf("✓ removed %s\n", plist)
		return nil
	}
	return fmt.Errorf("service uninstall is not supported on %s", runtime.GOOS)
}

func installSystemd(spec serviceSpec, exe string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\nDescription=%s\nAfter=network-online.target\nWants=network-online.target\n\n", spec.Description)
	fmt.Fprintf(&b, "[Service]\nExecStart=%s %s\n", exe, strings.Join(spec.Args, " "))
	if spec.User != "" && spec.User != "root" {
		fmt.Fprintf(&b, "User=%s\n", spec.User)
	}
	b.WriteString("Restart=on-failure\nRestartSec=5s\n" +
		"StateDirectory=citric\nCacheDirectory=citric\n" +
		"NoNewPrivileges=true\nProtectSystem=full\nPrivateTmp=true\n\n" +
		"[Install]\nWantedBy=multi-user.target\n")
	unit := "/etc/systemd/system/" + spec.Name + ".service"
	if err := os.WriteFile(unit, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("write %s (run as root?): %w", unit, err)
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", spec.Name}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	fmt.Printf("✓ installed and started %s (%s)\n  logs: journalctl -u %s -f\n", spec.Name, unit, spec.Name)
	return nil
}

func installLaunchd(spec serviceSpec, exe string) error {
	label := launchdLabelPrefix + spec.Name
	var args strings.Builder
	for _, a := range append([]string{exe}, spec.Args...) {
		fmt.Fprintf(&args, "    <string>%s</string>\n", xmlEscape(a))
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<!-- %s -->
<plist version="1.0">
<dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
%s  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/var/log/%s.log</string>
  <key>StandardErrorPath</key><string>/var/log/%s.log</string>
</dict>
</plist>
`, xmlEscape(spec.Description), label, args.String(), spec.Name, spec.Name)
	path := "/Library/LaunchDaemons/" + label + ".plist"
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write %s (run with sudo?): %w", path, err)
	}
	_ = exec.Command("launchctl", "bootout", "system", path).Run()
	if out, err := exec.Command("launchctl", "bootstrap", "system", path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v\n%s", err, out)
	}
	fmt.Printf("✓ installed and started %s (%s)\n  logs: /var/log/%s.log\n", label, path, spec.Name)
	return nil
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// runAsPlatformService is a no-op off Windows: systemd and launchd run the
// command as an ordinary process.
func runAsPlatformService(name string, run func() error) (handled bool, err error) {
	return false, nil
}
