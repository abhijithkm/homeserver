package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/abhijithkm/homeserver/internal/paths"
	"github.com/abhijithkm/homeserver/internal/update"
)

// cmdUpdate checks GitHub for a newer release, downloads the installer,
// verifies its SHA-256 against the release's checksum file and runs it.
func cmdUpdate(args []string, _ paths.Dirs) error {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	check := fs.Bool("check", false, "only report whether an update exists")
	yes := fs.Bool("yes", false, "install without asking")
	pre := fs.Bool("pre", false, "include pre-releases")
	pause := fs.Bool("pause", false, "wait for Enter before exiting (used by the tray icon)")
	_ = fs.Parse(args)
	if *pause {
		defer func() {
			fmt.Print("\nPress Enter to close...")
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		}()
	}

	cl := update.NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	info, err := cl.Check(ctx, version, *pre)
	cancel()
	if errors.Is(err, update.ErrDevBuild) {
		return err
	}
	if err != nil {
		return fmt.Errorf("cannot check for updates: %w", err)
	}
	if !info.Available {
		fmt.Printf("HomeServer %s is up to date.\n", version)
		return nil
	}
	kind := "release"
	if info.Prerelease {
		kind = "pre-release"
	}
	fmt.Printf("Update available: %s (%s), you have %s.\nRelease notes: %s\n", info.Latest, kind, version, info.PageURL)
	if *check {
		return nil
	}
	if err := needAdmin(); err != nil {
		return err
	}
	if !*yes {
		fmt.Print("Download and install it now? The service restarts briefly. [y/N] ")
		ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(ans)); a != "y" && a != "yes" {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	// The installer is staged next to homeserver.exe (Program Files: writable by
	// administrators only), not in the data folder, which the service account can
	// modify and could otherwise swap the file before it runs elevated.
	self, err := exePath()
	if err != nil {
		return err
	}
	dir := filepath.Join(filepath.Dir(self), "updates")
	_ = os.RemoveAll(dir)
	dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer dcancel()
	fmt.Println("Downloading...")
	installer, err := cl.Download(dctx, info, dir)
	if err != nil {
		return err
	}
	fmt.Println("Checksum verified. Starting the installer; HomeServer will restart when it finishes.")
	return launchInstaller(installer)
}
