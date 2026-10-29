package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// The service runs as LocalService, which cannot read folders such as
// C:\Users\<you>\Downloads until it is granted access. This menu action picks a
// folder with the standard Windows dialog and runs "homeserver.exe grant" for
// it. The owner of a folder may change its permissions, so the first attempt
// needs no elevation; only when that is refused is a UAC prompt shown.

const folderDialogScript = `Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = 'Choose a folder HomeServer should be allowed to read'
$d.ShowNewFolderButton = $false
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $d.SelectedPath }`

// psOut runs a PowerShell script (single-threaded apartment, as the Windows
// dialogs need) and returns its trimmed output. Values are passed through the
// environment, never spliced into the script.
func psOut(script string, env ...string) string {
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-WindowStyle", "Hidden", "-Command", script)
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

// messageBox shows a Windows message box and returns the button pressed
// ("OK", "Yes", "No", "Cancel"). buttons and icon are fixed names from this file.
func messageBox(text, buttons, icon string) string {
	script := `Add-Type -AssemblyName System.Windows.Forms
Write-Output ([System.Windows.Forms.MessageBox]::Show($env:HS_MSG, 'HomeServer', '` + buttons + `', '` + icon + `'))`
	return psOut(script, "HS_MSG="+text)
}

// grantArgs is the argument list for "homeserver.exe grant".
func grantArgs(path string, readOnly bool) []string {
	args := []string{"grant", path}
	if readOnly {
		args = append(args, "--readonly")
	}
	return args
}

func grantFolderAccess() {
	path := psOut(folderDialogScript)
	if path == "" {
		return // dialog cancelled
	}
	path = filepath.Clean(path)
	if filepath.Dir(path) == path {
		messageBox("Choose a folder, not a whole drive.", "OK", "Warning")
		return
	}
	var readOnly bool
	switch messageBox("Let HomeServer read this folder?\n\n"+path+
		"\n\nYes = read only (people can browse and download)"+
		"\nNo = read and write (people can also upload and change files)"+
		"\nCancel = stop", "YesNoCancel", "Question") {
	case "Yes":
		readOnly = true
	case "No":
	default:
		return
	}

	cmd := exec.Command(serverExe(), grantArgs(path, readOnly)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err == nil {
		messageBox(fmt.Sprintf("Done. HomeServer can now read:\n%s\n\nAdd it in the web page: Admin > Shares > Add share.", path), "OK", "Information")
		return
	} else if !strings.Contains(strings.ToLower(string(out)), "access denied") {
		messageBox("Could not change the folder's permissions:\n\n"+strings.TrimSpace(string(out)), "OK", "Error")
		return
	}
	// Not the folder's owner: ask Windows for administrator rights (UAC). The
	// console stays open (--pause) so the result can be read.
	args := grantArgs(path, readOnly)
	args[1] = `"` + path + `"`
	runElevated("Normal", append(args, "--pause")...)
}
