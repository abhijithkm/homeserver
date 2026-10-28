package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/abhijithkm/homeserver/internal/auth"
	"github.com/abhijithkm/homeserver/internal/netinfo"
	"github.com/abhijithkm/homeserver/internal/paths"
	"github.com/abhijithkm/homeserver/internal/store"
)

type runtimeInfo struct {
	PID     int      `json:"pid"`
	Port    int      `json:"port"`
	Scheme  string   `json:"scheme"`
	URLs    []string `json:"urls"`
	Version string   `json:"version"`
	Started int64    `json:"started"`
}

func readRuntime(d paths.Dirs) (runtimeInfo, error) {
	var ri runtimeInfo
	b, err := os.ReadFile(d.Runtime)
	if err != nil {
		return ri, err
	}
	return ri, json.Unmarshal(b, &ri)
}

func httpOK(url string) bool {
	c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: insecureTLS()}}
	resp, err := c.Get(url + "/healthz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func cmdStatus(d paths.Dirs) error {
	fmt.Println("Service:", serviceState())
	ri, err := readRuntime(d)
	if err != nil {
		fmt.Println("Server: not running (no runtime info)")
		return nil
	}
	local := fmt.Sprintf("%s://127.0.0.1:%d", ri.Scheme, ri.Port)
	if httpOK(local) {
		fmt.Println("Server: responding at", local)
		for _, u := range ri.URLs {
			fmt.Println("  LAN address:", u)
		}
	} else {
		fmt.Println("Server: runtime info present but not responding (stale file or stopped)")
	}
	return nil
}

func cmdLogs(args []string, d paths.Dirs) error {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	n := fs.Int("n", 100, "number of lines")
	_ = fs.Parse(args)
	f, err := os.Open(d.LogFile)
	if err != nil {
		return fmt.Errorf("no log file yet at %s", d.LogFile)
	}
	defer f.Close()
	ring := make([]string, 0, *n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		if len(ring) == *n {
			copy(ring, ring[1:])
			ring = ring[:*n-1]
		}
		ring = append(ring, sc.Text())
	}
	for _, l := range ring {
		fmt.Println(l)
	}
	return nil
}

func cmdDiagnose(d paths.Dirs) error {
	ri, err := readRuntime(d)
	if err != nil {
		fmt.Println("[FAIL] no runtime info: the server does not appear to be running. Try: homeserver status")
		return nil
	}
	scheme := ri.Scheme
	fmt.Printf("Configured port: %d (%s)\n", ri.Port, scheme)
	if httpOK(fmt.Sprintf("%s://127.0.0.1:%d", scheme, ri.Port)) {
		fmt.Println("[ OK ] server answers on this computer")
	} else {
		fmt.Println("[FAIL] server does not answer on 127.0.0.1 - it is stopped, crashed or the port is blocked. Check: homeserver logs")
	}
	addrs := netinfo.LocalIPv4()
	if len(addrs) == 0 {
		fmt.Println("[FAIL] no active IPv4 network address found - connect to Wi-Fi/Ethernet")
	}
	for _, a := range addrs {
		u := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(a.IP, strconv.Itoa(ri.Port)))
		if httpOK(u) {
			fmt.Printf("[ OK ] reachable via %s (%s)\n", u, a.Interface)
		} else {
			fmt.Printf("[FAIL] not reachable via %s (%s)\n", u, a.Interface)
		}
	}
	fmt.Println(firewallStatus())
	fmt.Println("Remember: other devices must be on the same Wi-Fi/LAN, the network profile should be Private, and router client isolation must be off.")
	fmt.Println("Note: this check runs on the server computer; a firewall block only shows up when you test from another device.")
	return nil
}

func openStore(d paths.Dirs) (*store.Store, error) {
	st, rec, err := store.Open(d.DB)
	if rec != "" {
		fmt.Fprintln(os.Stderr, "warning: database was corrupt; moved to", rec)
	}
	return st, err
}

func readPassword(prompt string) (string, error) {
	if os.Getenv("HOMESERVER_PASSWORD_STDIN") == "1" {
		l, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		return strings.TrimRight(l, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func cmdSetPassword(args []string, d paths.Dirs) error {
	if len(args) != 1 {
		return errors.New("usage: homeserver set-password USER   (set HOMESERVER_PASSWORD_STDIN=1 to read the password from stdin)")
	}
	user := args[0]
	if err := auth.ValidateUsername(user); err != nil {
		return err
	}
	pw, err := readPassword("New password: ")
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(pw); err != nil {
		return err
	}
	st, err := openStore(d)
	if err != nil {
		return err
	}
	defer st.Close()
	h, err := auth.Hash(pw)
	if err != nil {
		return err
	}
	if u, err := st.UserByName(user); err == nil {
		if err := st.SetPassword(u.ID, h); err != nil {
			return err
		}
		fmt.Println("Password updated for", u.Username, "(all its sessions were signed out).")
		return nil
	}
	if _, err := st.AddUser(user, h, "admin"); err != nil {
		return err
	}
	fmt.Println("Administrator", user, "created.")
	return nil
}

func cmdSetPort(args []string, d paths.Dirs) error {
	if len(args) != 1 {
		return errors.New("usage: homeserver set-port N")
	}
	p, err := strconv.Atoi(args[0])
	if err != nil {
		return errors.New("port must be a number")
	}
	st, err := openStore(d)
	if err != nil {
		return err
	}
	defer st.Close()
	s, err := st.GetSettings()
	if err != nil {
		return err
	}
	s.Port = p
	if err := st.SaveSettings(s); err != nil {
		return err
	}
	fmt.Printf("Port set to %d. Restart the service (homeserver restart) and update your firewall rule (homeserver firewall add).\n", p)
	return nil
}

func cmdBackup(args []string, d paths.Dirs) error {
	if len(args) != 1 {
		return errors.New("usage: homeserver backup FILE")
	}
	st, err := openStore(d)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.BackupTo(args[0]); err != nil {
		return err
	}
	fmt.Println("Backup written to", args[0], "(contains password hashes: keep it private).")
	return nil
}

func cmdRestore(args []string, d paths.Dirs) error {
	if len(args) != 1 {
		return errors.New("usage: homeserver restore FILE")
	}
	if ri, err := readRuntime(d); err == nil && httpOK(fmt.Sprintf("%s://127.0.0.1:%d", ri.Scheme, ri.Port)) {
		return errors.New("the server is running; stop it first (homeserver stop)")
	}
	in, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	// validate the candidate by opening a scratch copy
	scratch := d.DB + ".restore-check"
	if err := os.WriteFile(scratch, in, 0o600); err != nil {
		return err
	}
	chk, rec, err := store.Open(scratch)
	if err != nil || rec != "" {
		dropDB(scratch)
		return errors.New("the file is not a valid HomeServer database")
	}
	chk.Close()
	dropDB(scratch)
	if _, err := os.Stat(d.DB); err == nil {
		old := d.DB + ".before-restore-" + time.Now().Format("20060102-150405")
		if err := os.Rename(d.DB, old); err != nil {
			return err
		}
		fmt.Println("Previous database kept as", old)
	}
	_ = os.Remove(d.DB + "-wal")
	_ = os.Remove(d.DB + "-shm")
	if err := os.WriteFile(d.DB, in, 0o600); err != nil {
		return err
	}
	fmt.Println("Restored. Start the service: homeserver start")
	return nil
}

func dropDB(base string) {
	for _, s := range []string{"", "-wal", "-shm"} {
		_ = os.Remove(base + s)
	}
}
