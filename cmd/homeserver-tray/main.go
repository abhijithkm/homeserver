// Command homeserver-tray is a small notification-area helper for HomeServer.
// It only observes and controls the Windows service; the server never
// depends on it, and closing it does not stop file sharing.
package main

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
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

func healthy(ri runtimeInfo) bool {
	resp, err := client.Get(fmt.Sprintf("%s://127.0.0.1:%d/healthz", ri.Scheme, ri.Port))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
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
	ps := fmt.Sprintf(`Start-Process -FilePath '%s' -ArgumentList '%s' -Verb RunAs -WindowStyle %s`,
		strings.ReplaceAll(serverExe(), "'", "''"), strings.Join(args, " "), style)
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
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

// icon draws a simple house-in-a-disc glyph and wraps the PNG in an ICO container.
func icon(running bool) []byte {
	const n = 32
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	bg := color.RGBA{0x9a, 0xa0, 0xa6, 0xff}
	if running {
		bg = color.RGBA{0x1a, 0x73, 0xe8, 0xff}
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)-15.5, float64(y)-15.5
			if dx*dx+dy*dy <= 15.5*15.5 {
				img.Set(x, y, bg)
			}
		}
	}
	white := color.RGBA{0xff, 0xff, 0xff, 0xff}
	for y := 9; y < 24; y++ { // roof triangle + body
		half := 0
		if y < 16 {
			half = (y - 9) * 2
		} else {
			half = 11
		}
		for x := 16 - half; x < 16+half; x++ {
			if y >= 16 && (x < 9 || x > 22) {
				continue
			}
			if y < 16 || (x >= 9 && x <= 22) {
				img.Set(x, y, white)
			}
		}
	}
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, img)
	var ico bytes.Buffer
	_ = binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, 1})
	ico.Write([]byte{n, n, 0, 0})
	_ = binary.Write(&ico, binary.LittleEndian, [2]uint16{1, 32})
	_ = binary.Write(&ico, binary.LittleEndian, uint32(pngBuf.Len()))
	_ = binary.Write(&ico, binary.LittleEndian, uint32(22))
	ico.Write(pngBuf.Bytes())
	return ico.Bytes()
}

func main() {
	debug.SetGCPercent(20) // tiny heap; keep the optional helper lean
	systray.Run(onReady, func() {})
}

func onReady() {
	systray.SetTitle("HomeServer")
	systray.SetIcon(icon(false))
	status := systray.AddMenuItem("Checking...", "Server status")
	status.Disable()
	open := systray.AddMenuItem("Open HomeServer", "Open the web interface")
	copyAddr := systray.AddMenuItem("Copy network address", "Copy the address for other devices")
	systray.AddSeparator()
	start := systray.AddMenuItem("Start service", "Requires administrator approval")
	stop := systray.AddMenuItem("Stop service", "Requires administrator approval")
	restart := systray.AddMenuItem("Restart service", "Requires administrator approval")
	systray.AddSeparator()
	upd := systray.AddMenuItem("Check for updates...", "Finds a newer GitHub release; asks before installing")
	systray.AddSeparator()
	quit := systray.AddMenuItem("Close this icon (sharing continues)", "The server keeps running")

	var lastRunning *bool
	update := func() {
		ri, ok := readRuntime()
		running := ok && healthy(ri)
		if lastRunning == nil || *lastRunning != running {
			systray.SetIcon(icon(running))
			lastRunning = &running
		}
		if running {
			addr := ri.URLs
			text := "Running"
			if len(addr) > 0 {
				text += " - " + addr[0]
			}
			status.SetTitle(text)
			systray.SetTooltip("HomeServer: " + text)
		} else {
			status.SetTitle("Stopped - other devices cannot connect")
			systray.SetTooltip("HomeServer is not running")
		}
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
