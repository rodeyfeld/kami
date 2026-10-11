// Package uptime keeps each service's health history as daily buckets in SQLite, so the health
// page can show uptime over months even though kami restarts on every deploy.
package uptime

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Level is how a sample counts toward uptime
type Level int

const (
	Up Level = iota
	Degraded
	Down
)

// Sample is one service's level at one moment. Key is a component name or GroupKey(project).
type Sample struct {
	Key   string
	Level Level
}

// GroupKey keeps a project's row apart from a component with the same name, like kami/kami
func GroupKey(group string) string {
	return "group:" + group
}

// Day is the seconds kami saw a service at each level on one UTC day. Time kami wasn't
// running isn't counted, so a day can add up to less than 24h.
type Day struct {
	Date     time.Time
	Up       int64
	Degraded int64
	Down     int64
}

func (d Day) Total() int64 { return d.Up + d.Degraded + d.Down }

// Uptime is the share of observed time the service wasn't down; degraded counts as up, since
// a rolling deploy reads as degraded while it still serves
func (d Day) Uptime() float64 {
	if d.Total() == 0 {
		return 0
	}
	return 1 - float64(d.Down)/float64(d.Total())
}

// Sum adds up days, for the uptime over a whole window
func Sum(days []Day) Day {
	var total Day
	for _, d := range days {
		total.Up += d.Up
		total.Degraded += d.Degraded
		total.Down += d.Down
	}
	return total
}

const keepDays = 120

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	// One connection serializes writes, which is all one recorder needs
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS uptime_days (
		key      TEXT    NOT NULL,
		day      TEXT    NOT NULL,
		up       INTEGER NOT NULL DEFAULT 0,
		degraded INTEGER NOT NULL DEFAULT 0,
		down     INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (key, day)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create uptime_days: %w", err)
	}
	return &Store{db: db, now: time.Now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Record credits each sample with `seconds` of its level on today's bucket
func (s *Store) Record(ctx context.Context, samples []Sample, seconds int64) error {
	now := s.now().UTC()
	day := now.Format(time.DateOnly)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO uptime_days (key, day, up, degraded, down) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (key, day) DO UPDATE SET
			up = up + excluded.up, degraded = degraded + excluded.degraded, down = down + excluded.down`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, sample := range samples {
		var up, degraded, down int64
		switch sample.Level {
		case Up:
			up = seconds
		case Degraded:
			degraded = seconds
		default:
			down = seconds
		}
		if _, err := stmt.ExecContext(ctx, sample.Key, day, up, degraded, down); err != nil {
			return err
		}
	}

	cutoff := now.AddDate(0, 0, -keepDays).Format(time.DateOnly)
	if _, err := tx.ExecContext(ctx, `DELETE FROM uptime_days WHERE day < ?`, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

// History returns the last `days` days for every key, oldest first and ending today. Days
// without data come back zeroed so every key lines up.
func (s *Store) History(ctx context.Context, days int) (map[string][]Day, error) {
	today := s.now().UTC().Truncate(24 * time.Hour)
	first := today.AddDate(0, 0, -(days - 1))

	rows, err := s.db.QueryContext(ctx, `SELECT key, day, up, degraded, down FROM uptime_days WHERE day >= ?`,
		first.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	history := make(map[string][]Day)
	for rows.Next() {
		var key, day string
		var d Day
		if err := rows.Scan(&key, &day, &d.Up, &d.Degraded, &d.Down); err != nil {
			return nil, err
		}
		date, err := time.Parse(time.DateOnly, day)
		if err != nil {
			continue
		}
		i := int(date.Sub(first).Hours() / 24)
		if i < 0 || i >= days {
			continue
		}
		if history[key] == nil {
			history[key] = make([]Day, days)
			for j := range history[key] {
				history[key][j].Date = first.AddDate(0, 0, j)
			}
		}
		d.Date = date
		history[key][i] = d
	}
	return history, rows.Err()
}

// Since is the first day kami recorded anything, or zero when it hasn't yet
func (s *Store) Since(ctx context.Context) (time.Time, error) {
	var day sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(day) FROM uptime_days`).Scan(&day); err != nil {
		return time.Time{}, err
	}
	if !day.Valid {
		return time.Time{}, nil
	}
	return time.Parse(time.DateOnly, day.String)
}
