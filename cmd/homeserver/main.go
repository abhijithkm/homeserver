// Command homeserver is the HomeServer file-sharing server and its CLI.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/abhijithkm/homeserver/internal/paths"
)

var version = "dev"

const usage = `HomeServer ` + "%s" + ` - local-network file sharing

Usage: homeserver [--data DIR] <command> [args]

Server:
  run                     run the server in the foreground (default)
Service (Windows, administrator):
  install [--account A]   install and start the Windows service (default account: LocalService)
  uninstall               stop and remove the service
  start | stop | restart  control the service
  status                  show service and server status
  firewall add|remove     manage the Private-network firewall rule
  grant PATH [--readonly] give the service account access to a folder
Maintenance:
  logs [-n N]             print the last N log lines
  diagnose                check network accessibility
  set-password USER       create the admin or reset a user's password
  set-port N              change the listening port (restart service afterwards)
  backup FILE             write a consistent copy of the configuration database
  restore FILE            replace the configuration database (server must be stopped)
  update [--check] [--yes] [--pre]
                          find, verify and install a newer GitHub release (administrator)
  version
`

func main() {
	global := flag.NewFlagSet("homeserver", flag.ExitOnError)
	data := global.String("data", "", "data directory (default %ProgramData%\\HomeServer)")
	global.Usage = func() { fmt.Fprintf(os.Stderr, usage, version) }
	_ = global.Parse(os.Args[1:])
	args := global.Args()
	dirs := paths.Resolve(*data)

	if isWindowsService() {
		os.Exit(runAsService(dirs))
	}
	cmd := "run"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	if err := dispatch(cmd, args, dirs); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func dispatch(cmd string, args []string, d paths.Dirs) error {
	switch cmd {
	case "run":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := acquireSingleInstance(); err != nil {
			return err
		}
		return runServer(ctx, d, true)
	case "version":
		fmt.Println("homeserver", version)
		return nil
	case "help", "-h", "--help":
		fmt.Printf(usage, version)
		return nil
	case "install":
		return svcInstall(args, d)
	case "uninstall":
		return svcUninstall()
	case "start":
		return svcStart()
	case "stop":
		return svcStop()
	case "restart":
		return svcRestart()
	case "status":
		return cmdStatus(d)
	case "firewall":
		return cmdFirewall(args, d)
	case "grant":
		return cmdGrant(args)
	case "logs":
		return cmdLogs(args, d)
	case "diagnose":
		return cmdDiagnose(d)
	case "set-password":
		return cmdSetPassword(args, d)
	case "set-port":
		return cmdSetPort(args, d)
	case "backup":
		return cmdBackup(args, d)
	case "restore":
		return cmdRestore(args, d)
	case "update":
		return cmdUpdate(args, d)
	}
	fmt.Fprintf(os.Stderr, usage, version)
	return fmt.Errorf("unknown command %q", cmd)
}
