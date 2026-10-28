package store

import (
	"fmt"
	"strconv"
)

// Settings are application-wide options persisted in SQLite.
type Settings struct {
	Port           int    `json:"port"`
	BindAddr       string `json:"bindAddr"`
	MaxUploadBytes int64  `json:"maxUploadBytes"` // 0 = unlimited
	MaxTransfers   int    `json:"maxTransfers"`
	SessionHours   int    `json:"sessionHours"`
	TLSMode        string `json:"tlsMode"` // off | selfsigned | custom
	TLSCert        string `json:"tlsCert"`
	TLSKey         string `json:"tlsKey"`
	// UpdateCheck lets the admin page ask GitHub whether a newer release exists.
	// Nothing is downloaded or installed unless an administrator runs `homeserver update`.
	UpdateCheck      bool `json:"updateCheck"`
	UpdatePrerelease bool `json:"updatePrerelease"`
}

// DefaultSettings returns the factory defaults.
func DefaultSettings() Settings {
	return Settings{Port: 8686, BindAddr: "0.0.0.0", MaxTransfers: 16, SessionHours: 24, TLSMode: "off", UpdateCheck: true}
}

// Validate checks value ranges.
func (s Settings) Validate() error {
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("port must be 1-65535")
	}
	if s.MaxTransfers < 1 || s.MaxTransfers > 256 {
		return fmt.Errorf("maxTransfers must be 1-256")
	}
	if s.SessionHours < 1 || s.SessionHours > 24*90 {
		return fmt.Errorf("sessionHours must be 1-2160")
	}
	if s.MaxUploadBytes < 0 {
		return fmt.Errorf("maxUploadBytes must be >= 0")
	}
	switch s.TLSMode {
	case "off", "selfsigned":
	case "custom":
		if s.TLSCert == "" || s.TLSKey == "" {
			return fmt.Errorf("custom TLS needs certificate and key paths")
		}
	default:
		return fmt.Errorf("invalid tlsMode")
	}
	if s.BindAddr == "" {
		return fmt.Errorf("bindAddr required")
	}
	return nil
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// GetSettings loads settings, applying defaults for missing keys.
func (s *Store) GetSettings() (Settings, error) {
	out := DefaultSettings()
	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return out, err
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		switch k {
		case "port":
			out.Port = int(n)
		case "bindAddr":
			out.BindAddr = v
		case "maxUploadBytes":
			out.MaxUploadBytes = n
		case "maxTransfers":
			out.MaxTransfers = int(n)
		case "sessionHours":
			out.SessionHours = int(n)
		case "tlsMode":
			out.TLSMode = v
		case "tlsCert":
			out.TLSCert = v
		case "tlsKey":
			out.TLSKey = v
		case "updateCheck":
			out.UpdateCheck = v == "1"
		case "updatePrerelease":
			out.UpdatePrerelease = v == "1"
		}
	}
	return out, rows.Err()
}

// SaveSettings validates and persists settings atomically.
func (s *Store) SaveSettings(v Settings) error {
	if err := v.Validate(); err != nil {
		return err
	}
	kv := map[string]string{
		"port": strconv.Itoa(v.Port), "bindAddr": v.BindAddr,
		"maxUploadBytes": strconv.FormatInt(v.MaxUploadBytes, 10),
		"maxTransfers":   strconv.Itoa(v.MaxTransfers), "sessionHours": strconv.Itoa(v.SessionHours),
		"tlsMode": v.TLSMode, "tlsCert": v.TLSCert, "tlsKey": v.TLSKey,
		"updateCheck": boolStr(v.UpdateCheck), "updatePrerelease": boolStr(v.UpdatePrerelease),
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, val := range kv {
		if _, err := tx.Exec("INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", k, val); err != nil {
			return err
		}
	}
	return tx.Commit()
}
