// Command homeserver-tray is a small notification-area helper for HomeServer.
// It only observes and controls the Windows service; the server never
// depends on it, and closing it does not stop file sharing.
package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/getlantern/systray"
)

type runtimeInfo struct {
	Port   int      `json:"port"`
	Scheme string   `json:"scheme"`
	URLs   []string `json:"urls"`
}

func dataDir() string {
	if d := os.Getenv("HOMESERVER_DATA"); d != "" {
		return d
	}
	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}
	return filepath.Join(base, "HomeServer")
}

func readRuntime() (runtimeInfo, bool) {
	var ri runtimeInfo
	b, err := os.ReadFile(filepath.Join(dataDir(), "runtime.json"))
	if err != nil || json.Unmarshal(b, &ri) != nil {
		return ri, false
	}
	return ri, true
}

var client = &http.Client{
	Timeout:   2 * time.Second,
	Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, // loopback health probe only
}

func healthy(scheme string, port int) bool {
	resp, err := client.Get(fmt.Sprintf("%s://127.0.0.1:%d/healthz", scheme, port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// probe checks that the server answers on this machine. runtime.json is only
// readable when the tray runs with access to the data folder, so without it the
// default port is tried over both schemes.
func probe() (bool, runtimeInfo) {
	if ri, ok := readRuntime(); ok {
		return healthy(ri.Scheme, ri.Port), ri
	}
	for _, scheme := range []string{"http", "https"} {
		if healthy(scheme, 8686) {
			return true, runtimeInfo{Port: 8686, Scheme: scheme}
		}
	}
	return false, runtimeInfo{}
}

func serverExe() string {
	self, _ := os.Executable()
	return filepath.Join(filepath.Dir(self), "homeserver.exe")
}

// elevated runs "homeserver.exe <args>" with a UAC prompt (service control
// needs administrator rights; the tray itself runs unprivileged).
func elevated(args ...string) { runElevated("Hidden", args...) }

// runElevated is elevated with a chosen window style; "Normal" lets the user
// read and answer a console prompt (used for updates).
func runElevated(style string, args ...string) {
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", elevatedCommand(serverExe(), style, args...))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

// elevatedCommand is the PowerShell that starts exe with args under a UAC
// prompt. Single quotes are doubled so a folder name cannot break out of the
// quoted strings.
func elevatedCommand(exe, style string, args ...string) string {
	q := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	return fmt.Sprintf(`Start-Process -FilePath '%s' -ArgumentList '%s' -Verb RunAs -WindowStyle %s`,
		q(exe), q(strings.Join(args, " ")), style)
}

func openBrowser(url string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}

func copyText(s string) {
	cmd := exec.Command("clip")
	cmd.Stdin = strings.NewReader(s)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
}

func main() {
	debug.SetGCPercent(20) // tiny heap; keep the optional helper lean
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetTitle("HomeServer")
	pics := icons()
	systray.SetIcon(pics[stChecking])
	status := systray.AddMenuItem(stChecking.label(""), "Server status")
	status.Disable()
	open := systray.AddMenuItem("Open HomeServer", "Open the web interface")
	copyAddr := systray.AddMenuItem("Copy network address", "Copy the address for other devices")
	grant := systray.AddMenuItem("Grant folder access...", "Let HomeServer read a folder (needed before sharing it)")
	systray.AddSeparator()
	start := systray.AddMenuItem("Start service", "Requires administrator approval")
	stop := systray.AddMenuItem("Stop service", "Requires administrator approval")
	restart := systray.AddMenuItem("Restart service", "Requires administrator approval")
	systray.AddSeparator()
	upd := systray.AddMenuItem("Check for updates...", "Finds a newer GitHub release; asks before installing")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Close this icon (sharing continues)", "The server keeps running")

	last := stChecking
	update := func() {
		var ri runtimeInfo
		check := func() bool {
			ok, info := probe()
			ri = info
			return ok
		}
		var cur state
		if svc, err := queryService(); err != nil {
			// Cannot ask Windows: the health probe alone decides.
			cur = stStopped
			if check() {
				cur = stRunning
			}
		} else {
			cur = decide(svc, check)
		}
		addr := ""
		if len(ri.URLs) > 0 {
			addr = ri.URLs[0]
		}
		text := cur.label(addr)
		if cur != last {
			systray.SetIcon(pics[cur])
			last = cur
		}
		status.SetTitle(text)
		systray.SetTooltip("HomeServer: " + text)
	}
	update()
	tick := time.NewTicker(5 * time.Second)
	go func() {
		for {
			select {
			case <-tick.C:
				update()
			case <-open.ClickedCh:
				if ri, ok := readRuntime(); ok {
					openBrowser(fmt.Sprintf("%s://localhost:%d/", ri.Scheme, ri.Port))
				} else {
					openBrowser("http://localhost:8686/")
				}
			case <-copyAddr.ClickedCh:
				if ri, ok := readRuntime(); ok && len(ri.URLs) > 0 {
					copyText(ri.URLs[0])
				}
			case <-grant.ClickedCh:
				go grantFolderAccess() // shows dialogs; must not block the status loop
			case <-start.ClickedCh:
				elevated("start")
			case <-stop.ClickedCh:
				elevated("stop")
			case <-restart.ClickedCh:
				elevated("restart")
			case <-upd.ClickedCh:
				runElevated("Normal", "update", "--pause")
			case <-quit.ClickedCh:
				tick.Stop()
				systray.Quit()
				return
			}
		}
	}()
}
