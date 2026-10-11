//go:build windows

package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func installService(spec serviceSpec) error {
	exe, err := executablePath()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator?): %w", err)
	}
	defer m.Disconnect()
	if s, err := m.OpenService(spec.Name); err == nil {
		s.Close()
		return fmt.Errorf("service %q already exists; run 'citric service uninstall' first", spec.Name)
	}
	s, err := m.CreateService(spec.Name, exe, mgr.Config{
		DisplayName: spec.DisplayName,
		Description: spec.Description,
		StartType:   mgr.StartAutomatic,
	}, spec.Args...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
	}, 86400)
	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	fmt.Printf("✓ installed and started Windows service %q\n", spec.Name)
	return nil
}

func uninstallService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator?): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("service %q not installed", name)
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	fmt.Printf("✓ removed Windows service %q\n", name)
	return nil
}

// runAsPlatformService runs fn under the Windows service manager when this
// process was started as a service, translating Stop/Shutdown into a clean
// exit. handled is false for an ordinary console launch.
func runAsPlatformService(name string, fn func() error) (handled bool, err error) {
	isSvc, err := svc.IsWindowsService()
	if err != nil || !isSvc {
		return false, nil
	}
	return true, svc.Run(name, &serviceHandler{fn: fn})
}

type serviceHandler struct{ fn func() error }

func (h *serviceHandler) Execute(args []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	done := make(chan error, 1)
	go func() { done <- h.fn() }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return false, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				return false, 0
			}
		}
	}
}
