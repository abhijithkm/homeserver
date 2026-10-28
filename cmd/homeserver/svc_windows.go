//go:build windows

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"golang.org/x/term"

	"github.com/abhijithkm/homeserver/internal/paths"
)

const (
	serviceName    = "HomeServer"
	serviceDisplay = "HomeServer file sharing"
	serviceDesc    = "Shares registered folders with devices on the local network."
	defaultAccount = `NT AUTHORITY\LocalService`
	firewallRule   = "HomeServer"
)

func insecureTLS() *tls.Config { return &tls.Config{InsecureSkipVerify: true} } // local health probe only

func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// acquireSingleInstance prevents two servers (service plus a foreground run)
// from running against the same data at once.
func acquireSingleInstance() error {
	name, _ := windows.UTF16PtrFromString(`Global\HomeServerSingleInstance`)
	_, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return errors.New("HomeServer is already running (service or another window); stop it first")
	}
	return nil // other errors (e.g. no permission for Global\) must not block startup
}

type winService struct{ d paths.Dirs }

func (w *winService) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runServer(ctx, w.d, false) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 30000}
				cancel()
				select {
				case <-done:
				case <-time.After(25 * time.Second):
				}
				return false, 0
			}
		case err := <-done:
			if err != nil {
				return true, 1 // non-zero: SCM failure actions restart us
			}
			return false, 0
		}
	}
}

func runAsService(d paths.Dirs) int {
	if err := acquireSingleInstance(); err != nil {
		return 1
	}
	if err := svc.Run(serviceName, &winService{d}); err != nil {
		return 1
	}
	return 0
}

func needAdmin() error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("this command needs an elevated (Run as administrator) terminal")
	}
	return nil
}

func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Abs(p)
}

func run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func svcInstall(args []string, d paths.Dirs) error {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	account := fs.String("account", defaultAccount, `service account, e.g. "NT AUTHORITY\LocalService" or ".\username"`)
	_ = fs.Parse(args)
	if err := needAdmin(); err != nil {
		return err
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d.Data, 0o700); err != nil {
		return err
	}
	// Restrict the data folder (database with password hashes, TLS key) to
	// administrators, SYSTEM and the service account.
	if out, err := run("icacls", d.Data, "/inheritance:r", "/grant:r",
		"SYSTEM:(OI)(CI)F", "Administrators:(OI)(CI)F", *account+":(OI)(CI)M"); err != nil {
		return fmt.Errorf("cannot set permissions on %s: %v: %s", d.Data, err, out)
	}
	password := ""
	if !strings.HasPrefix(strings.ToLower(*account), "nt authority") {
		password = os.Getenv("HOMESERVER_SERVICE_PASSWORD")
		if password == "" {
			fmt.Fprintf(os.Stderr, "Password for %s: ", *account)
			b, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return err
			}
			password = string(b)
		}
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	cfg := mgr.Config{
		DisplayName: serviceDisplay, Description: serviceDesc, StartType: mgr.StartAutomatic,
		DelayedAutoStart: true, ServiceStartName: *account, Password: password,
	}
	s, err := m.OpenService(serviceName)
	if err == nil { // upgrade in place
		defer s.Close()
		_ = stopAndWait(s)
		cfg.BinaryPathName = fmt.Sprintf(`"%s" --data "%s"`, exe, d.Data)
		if err := s.UpdateConfig(cfg); err != nil {
			return err
		}
	} else {
		s, err = m.CreateService(serviceName, exe, cfg, "--data", d.Data)
		if err != nil {
			return err
		}
		defer s.Close()
	}
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}, 86400)
	_ = s.SetRecoveryActionsOnNonCrashFailures(true)
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("service installed but did not start: %w (see: homeserver logs)", err)
	}
	fmt.Printf("Service %q installed (account %s, automatic start) and started.\n", serviceName, *account)
	fmt.Println("Open http://localhost:8686 on this computer to finish setup. Allow other devices with: homeserver firewall add")
	return nil
}

func stopAndWait(s *mgr.Service) error {
	st, err := s.Query()
	if err != nil || st.State == svc.Stopped {
		return err
	}
	if _, err := s.Control(svc.Stop); err != nil {
		return err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		st, err = s.Query()
		if err != nil || st.State == svc.Stopped {
			return err
		}
		time.Sleep(300 * time.Millisecond)
	}
	return errors.New("timed out waiting for the service to stop")
}

func withService(fn func(s *mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return errors.New("the HomeServer service is not installed (run: homeserver install)")
	}
	defer s.Close()
	return fn(s)
}

func svcUninstall() error {
	if err := needAdmin(); err != nil {
		return err
	}
	return withService(func(s *mgr.Service) error {
		_ = stopAndWait(s)
		if err := s.Delete(); err != nil {
			return err
		}
		fmt.Println("Service removed. Shared folder registrations and files were not touched (data folder kept).")
		return nil
	})
}

func svcStart() error {
	if err := needAdmin(); err != nil {
		return err
	}
	return withService(func(s *mgr.Service) error {
		if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return err
		}
		fmt.Println("Service started.")
		return nil
	})
}

func svcStop() error {
	if err := needAdmin(); err != nil {
		return err
	}
	return withService(func(s *mgr.Service) error {
		if err := stopAndWait(s); err != nil {
			return err
		}
		fmt.Println("Service stopped.")
		return nil
	})
}

func svcRestart() error {
	if err := needAdmin(); err != nil {
		return err
	}
	return withService(func(s *mgr.Service) error {
		if err := stopAndWait(s); err != nil {
			return err
		}
		if err := s.Start(); err != nil {
			return err
		}
		fmt.Println("Service restarted.")
		return nil
	})
}

// serviceState uses read-only rights so it works without elevation.
func serviceState() string {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return "unknown (cannot query Service Control Manager)"
	}
	defer windows.CloseServiceHandle(m)
	name, _ := windows.UTF16PtrFromString(serviceName)
	h, err := windows.OpenService(m, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return "not installed"
		}
		return "unknown (" + err.Error() + ")"
	}
	defer windows.CloseServiceHandle(h)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return "unknown"
	}
	names := map[uint32]string{windows.SERVICE_STOPPED: "stopped", windows.SERVICE_START_PENDING: "starting",
		windows.SERVICE_STOP_PENDING: "stopping", windows.SERVICE_RUNNING: "running",
		windows.SERVICE_CONTINUE_PENDING: "resuming", windows.SERVICE_PAUSE_PENDING: "pausing", windows.SERVICE_PAUSED: "paused"}
	if n, ok := names[st.CurrentState]; ok {
		return n
	}
	return "unknown"
}

// launchInstaller starts the verified installer detached from this process, so
// homeserver.exe is no longer running when Setup replaces it.
func launchInstaller(path string) error {
	cmd := exec.Command(path, "/SILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CLOSEAPPLICATIONS")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
	return cmd.Start()
}

func cmdFirewall(args []string, _ paths.Dirs) error {
	if len(args) != 1 || (args[0] != "add" && args[0] != "remove") {
		return errors.New("usage: homeserver firewall add|remove")
	}
	if err := needAdmin(); err != nil {
		return err
	}
	_, _ = run("netsh", "advfirewall", "firewall", "delete", "rule", "name="+firewallRule)
	if args[0] == "remove" {
		fmt.Println("Firewall rule removed.")
		return nil
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	// Program-based rule, Private networks only: survives port changes and
	// never opens the server on Public networks.
	out, err := run("netsh", "advfirewall", "firewall", "add", "rule", "name="+firewallRule, "dir=in", "action=allow",
		"program="+exe, "enable=yes", "profile=private", "protocol=TCP")
	if err != nil {
		return fmt.Errorf("netsh failed: %v: %s", err, out)
	}
	fmt.Println("Firewall rule added: inbound TCP to homeserver.exe on Private networks only.")
	return nil
}

func firewallStatus() string {
	out, err := run("netsh", "advfirewall", "firewall", "show", "rule", "name="+firewallRule)
	if err != nil || strings.Contains(out, "No rules match") {
		return "[WARN] no Windows Firewall rule named \"HomeServer\" (other devices may be blocked). Fix: homeserver firewall add   (as administrator)"
	}
	return "[ OK ] Windows Firewall rule \"HomeServer\" exists"
}

func cmdGrant(args []string) error {
	fs := flag.NewFlagSet("grant", flag.ExitOnError)
	ro := fs.Bool("readonly", false, "grant read access only")
	account := fs.String("account", defaultAccount, "service account")
	var path string
	var rest []string
	for _, a := range args { // allow the path before or after flags
		if !strings.HasPrefix(a, "-") && path == "" {
			path = a
		} else {
			rest = append(rest, a)
		}
	}
	_ = fs.Parse(rest)
	if path == "" {
		return errors.New(`usage: homeserver grant PATH [--readonly] [--account "NT AUTHORITY\LocalService"]`)
	}
	if err := needAdmin(); err != nil {
		return err
	}
	perm, word := "M", "modify"
	if *ro {
		perm, word = "RX", "read"
	}
	out, err := run("icacls", path, "/grant", fmt.Sprintf("%s:(OI)(CI)%s", *account, perm))
	if err != nil {
		return fmt.Errorf("icacls failed: %v: %s", err, out)
	}
	fmt.Printf("Granted %s access to %s for %s.\n", word, path, *account)
	return nil
}
