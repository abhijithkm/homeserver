package store

// AuditEvent is a security/configuration event.
type AuditEvent struct {
	TS     int64  `json:"ts"`
	Actor  string `json:"actor"`
	IP     string `json:"ip"`
	Event  string `json:"event"`
	Detail string `json:"detail"`
}

// Transfer is a completed or failed upload/download.
type Transfer struct {
	TS        int64  `json:"ts"`
	User      string `json:"user"`
	Share     string `json:"share"`
	Path      string `json:"path"`
	Direction string `json:"direction"` // upload | download
	Bytes     int64  `json:"bytes"`
	Status    string `json:"status"` // ok | aborted | error
	IP        string `json:"ip"`
}

const (
	maxAudit     = 2000
	maxTransfers = 500
)

// AddAudit records an event and trims old rows.
func (s *Store) AddAudit(actor, ip, event, detail string) error {
	if _, err := s.db.Exec("INSERT INTO audit(ts,actor,ip,event,detail) VALUES(?,?,?,?,?)", now(), actor, ip, event, detail); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM audit WHERE id <= (SELECT MAX(id) FROM audit) - ?", maxAudit)
	return err
}

// RecentAudit returns newest events first.
func (s *Store) RecentAudit(limit int) ([]AuditEvent, error) {
	rows, err := s.db.Query("SELECT ts,actor,ip,event,detail FROM audit ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		if err := rows.Scan(&e.TS, &e.Actor, &e.IP, &e.Event, &e.Detail); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddTransfer records a transfer outcome and trims old rows.
func (s *Store) AddTransfer(t Transfer) error {
	if _, err := s.db.Exec("INSERT INTO transfers(ts,username,share,path,direction,bytes,status,ip) VALUES(?,?,?,?,?,?,?,?)",
		now(), t.User, t.Share, t.Path, t.Direction, t.Bytes, t.Status, t.IP); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM transfers WHERE id <= (SELECT MAX(id) FROM transfers) - ?", maxTransfers)
	return err
}

// RecentTransfers returns newest first.
func (s *Store) RecentTransfers(limit int) ([]Transfer, error) {
	rows, err := s.db.Query("SELECT ts,username,share,path,direction,bytes,status,ip FROM transfers ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Transfer{}
	for rows.Next() {
		var t Transfer
		if err := rows.Scan(&t.TS, &t.User, &t.Share, &t.Path, &t.Direction, &t.Bytes, &t.Status, &t.IP); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
