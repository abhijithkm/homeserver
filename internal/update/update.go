// Package update finds, downloads and verifies new HomeServer releases
// published on GitHub. It never executes anything: the caller decides what to
// do with a verified installer.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// Repo is the only repository updates are accepted from.
	Repo = "abhijithkm/homeserver"

	maxInstallerBytes = 128 << 20
	maxSumsBytes      = 64 << 10
	maxAPIBytes       = 4 << 20
)

// ErrDevBuild means the running version is not a release and cannot be compared.
var ErrDevBuild = errors.New("this is a development build; updates are not checked")

// Info describes the newest applicable release.
type Info struct {
	Current       string    `json:"current"`
	Latest        string    `json:"latest"`
	Available     bool      `json:"available"`
	Prerelease    bool      `json:"prerelease"`
	PageURL       string    `json:"pageUrl,omitempty"`
	CheckedAt     time.Time `json:"checkedAt"`
	installerName string
	installerURL  string
	sumsURL       string
}

// Client talks to the GitHub releases API.
type Client struct {
	HTTP    *http.Client
	APIBase string // default https://api.github.com
	// TrustAsset decides whether a release asset link may be downloaded.
	// Nil means only this repository's github.com release downloads.
	TrustAsset func(rawURL string) bool
}

func (c *Client) trusted(raw string) bool {
	if c.TrustAsset != nil {
		return c.TrustAsset(raw)
	}
	return releaseAsset(raw)
}

// NewClient returns a client whose redirects are restricted to GitHub hosts over HTTPS.
func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if !allowedHost(req.URL) {
				return fmt.Errorf("redirect to %s refused", req.URL.Host)
			}
			return nil
		},
	}, APIBase: "https://api.github.com"}
}

func allowedHost(u *url.URL) bool {
	if u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "github.com" || h == "api.github.com" || strings.HasSuffix(h, ".githubusercontent.com")
}

type ghRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	HTMLURL    string `json:"html_url"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Check returns the newest release newer than current. Pre-releases are
// considered when includePre is set or when current is itself a pre-release.
func (c *Client) Check(ctx context.Context, current string, includePre bool) (Info, error) {
	info := Info{Current: current, CheckedAt: time.Now().UTC()}
	cur, ok := parseVersion(current)
	if !ok {
		return info, ErrDevBuild
	}
	includePre = includePre || cur.pre != ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIBase+"/repos/"+Repo+"/releases?per_page=30", nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HomeServer-updater")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var rels []ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAPIBytes)).Decode(&rels); err != nil {
		return info, fmt.Errorf("unreadable release list: %w", err)
	}
	info.Latest = current
	best := cur
	for _, r := range rels {
		v, ok := parseVersion(r.Tag)
		if r.Draft || !ok || (r.Prerelease && !includePre) || (v.pre != "") != r.Prerelease {
			continue
		}
		if compare(v, best) <= 0 {
			continue
		}
		name := "HomeServer-Setup-" + v.String() + ".exe"
		var inst, sums string
		for _, a := range r.Assets {
			switch {
			case a.Name == name:
				inst = a.URL
			case strings.HasPrefix(a.Name, "SHA256SUMS") && strings.HasSuffix(a.Name, ".txt"):
				sums = a.URL
			}
		}
		if inst == "" || sums == "" || !c.trusted(inst) || !c.trusted(sums) {
			continue // incomplete release: never offer it
		}
		best = v
		info.Latest, info.Available, info.Prerelease = v.String(), true, r.Prerelease
		info.PageURL, info.installerName, info.installerURL, info.sumsURL = r.HTMLURL, name, inst, sums
	}
	return info, nil
}

// releaseAsset accepts only download links inside this repository's releases.
func releaseAsset(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() == "github.com" &&
		strings.HasPrefix(u.Path, "/"+Repo+"/releases/download/")
}

// Download fetches the installer into dir, verifies its SHA-256 against the
// release's checksum file and returns its path. A mismatch deletes the file.
func (c *Client) Download(ctx context.Context, info Info, dir string) (string, error) {
	if !info.Available || info.installerURL == "" {
		return "", errors.New("no update to download")
	}
	sumsBody, err := c.fetch(ctx, info.sumsURL, maxSumsBytes)
	if err != nil {
		return "", fmt.Errorf("checksum file: %w", err)
	}
	want, err := findSum(string(sumsBody), info.installerName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, info.installerName)
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	werr := c.stream(ctx, info.installerURL, maxInstallerBytes, io.MultiWriter(f, h))
	cerr := f.Close()
	if werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(dst)
		return "", werr
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		_ = os.Remove(dst)
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s; download discarded", want, got)
	}
	return dst, nil
}

func (c *Client) fetch(ctx context.Context, u string, limit int64) ([]byte, error) {
	var sb strings.Builder
	if err := c.stream(ctx, u, limit, &sb); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func (c *Client) stream(ctx context.Context, u string, limit int64, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "HomeServer-updater")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download answered %s", resp.Status)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return errors.New("download larger than allowed")
	}
	return nil
}

// findSum reads "sha256  filename" lines (sha256sum / PowerShell style).
func findSum(body, name string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			h := strings.ToLower(f[0])
			if _, err := hex.DecodeString(h); err != nil || len(h) != 64 {
				return "", errors.New("malformed checksum line for " + name)
			}
			return h, nil
		}
	}
	return "", errors.New("no checksum listed for " + name)
}

type version struct {
	major, minor, patch int
	pre                 string
}

func (v version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
	if v.pre != "" {
		s += "-" + v.pre
	}
	return s
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || p == "" {
			return version{}, false
		}
		n[i] = v
	}
	return version{n[0], n[1], n[2], pre}, true
}

// splitNum splits "rc10" into ("rc", 10); the number is -1 when there is no numeric suffix.
func splitNum(s string) (string, int) {
	i := len(s)
	for i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
		i--
	}
	if i == len(s) {
		return s, -1
	}
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return s, -1
	}
	return s[:i], n
}

// compare follows semver precedence: a pre-release sorts before its release.
func compare(a, b version) int {
	for _, d := range [][2]int{{a.major, b.major}, {a.minor, b.minor}, {a.patch, b.patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	}
	ai, bi := strings.Split(a.pre, "."), strings.Split(b.pre, ".")
	for i := 0; i < len(ai) && i < len(bi); i++ {
		an, aerr := strconv.Atoi(ai[i])
		bn, berr := strconv.Atoi(bi[i])
		switch {
		case aerr == nil && berr == nil && an != bn:
			if an < bn {
				return -1
			}
			return 1
		case aerr == nil && berr != nil:
			return -1 // numeric identifiers sort before alphanumeric
		case aerr != nil && berr == nil:
			return 1
		case aerr != nil && berr != nil && ai[i] != bi[i]:
			// "rc10" must follow "rc2", which plain semver ordering gets wrong.
			ap, as := splitNum(ai[i])
			bp, bs := splitNum(bi[i])
			if ap == bp && as >= 0 && bs >= 0 {
				if as < bs {
					return -1
				}
				return 1
			}
			if ai[i] < bi[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ai) < len(bi):
		return -1
	case len(ai) > len(bi):
		return 1
	}
	return 0
}
