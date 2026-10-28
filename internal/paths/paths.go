// Package paths resolves HomeServer's on-disk locations.
package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// Dirs holds resolved locations.
type Dirs struct {
	Data    string
	DB      string
	LogDir  string
	LogFile string
	Runtime string // runtime.json read by the tray app
	TLS     string
}

// Resolve returns locations under data (or the platform default when empty).
func Resolve(data string) Dirs {
	if data == "" {
		data = os.Getenv("HOMESERVER_DATA")
	}
	if data == "" {
		if runtime.GOOS == "windows" {
			base := os.Getenv("ProgramData")
			if base == "" {
				base = `C:\ProgramData`
			}
			data = filepath.Join(base, "HomeServer")
		} else {
			home, _ := os.UserHomeDir()
			data = filepath.Join(home, ".homeserver")
		}
	}
	data, _ = filepath.Abs(data)
	return Dirs{
		Data:    data,
		DB:      filepath.Join(data, "homeserver.db"),
		LogDir:  filepath.Join(data, "logs"),
		LogFile: filepath.Join(data, "logs", "homeserver.log"),
		Runtime: filepath.Join(data, "runtime.json"),
		TLS:     filepath.Join(data, "tls"),
	}
}
