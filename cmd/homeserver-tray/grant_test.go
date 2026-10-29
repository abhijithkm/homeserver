package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestGrantArgs(t *testing.T) {
	if got := grantArgs(`C:\Users\me\Downloads`, true); !reflect.DeepEqual(got, []string{"grant", `C:\Users\me\Downloads`, "--readonly"}) {
		t.Errorf("read-only args = %v", got)
	}
	if got := grantArgs(`D:\Photos`, false); !reflect.DeepEqual(got, []string{"grant", `D:\Photos`}) {
		t.Errorf("read-write args = %v", got)
	}
}

// A folder name travels to PowerShell through the environment, so quotes and
// shell metacharacters in it come back unchanged and are never executed.
func TestPsOutPassesValuesThroughEnvironment(t *testing.T) {
	const tricky = `C:\Bob's "Photos"; Write-Output pwned $(1+1) & more`
	if got := psOut(`Write-Output $env:HS_TEST`, "HS_TEST="+tricky); got != tricky {
		t.Errorf("got %q, want %q", got, tricky)
	}
}

func TestElevatedCommandQuoting(t *testing.T) {
	got := elevatedCommand(`C:\Program Files\HomeServer\homeserver.exe`, "Normal", "grant", `"D:\Bob's Photos"`, "--readonly", "--pause")
	want := `Start-Process -FilePath 'C:\Program Files\HomeServer\homeserver.exe' -ArgumentList 'grant "D:\Bob''s Photos" --readonly --pause' -Verb RunAs -WindowStyle Normal`
	if got != want {
		t.Errorf("\n got: %s\nwant: %s", got, want)
	}
	// A quote in a folder name must not terminate the PowerShell string early.
	evil := elevatedCommand(`C:\x\homeserver.exe`, "Hidden", `a'; calc; '`)
	if strings.Contains(evil, `'a'; calc`) || !strings.Contains(evil, `a''; calc; ''`) {
		t.Errorf("single quote not doubled: %s", evil)
	}
}
