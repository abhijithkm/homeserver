package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

const serviceName = "HomeServer"

// state is what the tray icon and menu report.
type state int

const (
	stChecking state = iota // before the first probe
	stRunning               // service up and the server answers
	stBusy                  // service is starting or stopping
	stStopped               // service installed but not running
	stProblem               // service running but the server does not answer
	stMissing               // service not installed and nothing answers
)

// svcInfo is the Windows service state as seen without administrator rights.
type svcInfo struct {
	installed bool
	state     uint32 // windows.SERVICE_* current state, valid when installed
}

// queryService asks the service control manager for the state of the service.
// Only SERVICE_QUERY_STATUS is requested, which ordinary users are allowed, so
// the unprivileged tray does not depend on reading the locked-down data folder.
func queryService() (svcInfo, error) {
	m, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return svcInfo{}, err
	}
	defer windows.CloseServiceHandle(m)
	name, err := windows.UTF16PtrFromString(serviceName)
	if err != nil {
		return svcInfo{}, err
	}
	h, err := windows.OpenService(m, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return svcInfo{}, nil
		}
		return svcInfo{}, err
	}
	defer windows.CloseServiceHandle(h)
	var st windows.SERVICE_STATUS
	if err := windows.QueryServiceStatus(h, &st); err != nil {
		return svcInfo{}, err
	}
	return svcInfo{installed: true, state: st.CurrentState}, nil
}

// decide combines the service state with a health probe. probe is only called
// when the answer depends on it.
func decide(s svcInfo, probe func() bool) state {
	if !s.installed {
		// A copy started by hand (homeserver run) is still a running server.
		if probe() {
			return stRunning
		}
		return stMissing
	}
	switch s.state {
	case windows.SERVICE_START_PENDING, windows.SERVICE_CONTINUE_PENDING,
		windows.SERVICE_STOP_PENDING, windows.SERVICE_PAUSE_PENDING:
		return stBusy
	case windows.SERVICE_RUNNING:
		if probe() {
			return stRunning
		}
		return stProblem
	default: // stopped or paused
		return stStopped
	}
}

// label is the short text for the status menu item and tooltip.
func (s state) label(addr string) string {
	switch s {
	case stRunning:
		if addr != "" {
			return "Running - " + addr
		}
		return "Running"
	case stBusy:
		return "Starting or stopping..."
	case stStopped:
		return "Stopped - other devices cannot connect"
	case stProblem:
		return "Service is running but not answering - see Diagnostics"
	case stMissing:
		return "Not installed - run the installer"
	}
	return "Checking..."
}
