//go:build !windows

package main

import (
	"crypto/tls"
	"errors"

	"github.com/abhijithkm/homeserver/internal/paths"
)

var errWindowsOnly = errors.New("this command is only available on Windows")

func insecureTLS() *tls.Config { return &tls.Config{InsecureSkipVerify: true} } // local health probe only

func isWindowsService() bool                 { return false }
func runAsService(d paths.Dirs) int          { return 1 }
func acquireSingleInstance() error           { return nil }
func svcInstall([]string, paths.Dirs) error  { return errWindowsOnly }
func svcUninstall() error                    { return errWindowsOnly }
func svcStart() error                        { return errWindowsOnly }
func svcStop() error                         { return errWindowsOnly }
func svcRestart() error                      { return errWindowsOnly }
func serviceState() string                   { return "not supported on this OS" }
func firewallStatus() string                 { return "" }
func cmdFirewall([]string, paths.Dirs) error { return errWindowsOnly }
func cmdGrant([]string) error                { return errWindowsOnly }
func needAdmin() error                       { return errWindowsOnly }
func exePath() (string, error)               { return "", errWindowsOnly }
func launchInstaller(string) error           { return errWindowsOnly }
