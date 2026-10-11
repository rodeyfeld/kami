package health

import (
	"fmt"
	"kami/internal/uptime"
	"math"
	"strings"
	"time"
)

const (
	UptimeDays = 90
	mobileDays = 30 // phones only have room for the last month of bars
)

// History is the uptime window the page shows; a nil *History means kami isn't keeping one
type History struct {
	Days  map[string][]uptime.Day
	Since time.Time // first day with data, zero when nothing is recorded yet
}

// days returns a key's window, padded with empty days when kami has nothing for it
func (h *History) days(key string) []uptime.Day {
	if h != nil {
		if days, ok := h.Days[key]; ok {
			return days
		}
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	days := make([]uptime.Day, UptimeDays)
	for i := range days {
		days[i].Date = today.AddDate(0, 0, i-(UptimeDays-1))
	}
	return days
}

// overall averages the projects' uptime, so a busy project doesn't outweigh a quiet one
func (h *History) overall(groups []resourceGroup) (float64, bool) {
	if h == nil {
		return 0, false
	}
	var sum float64
	var n int
	for _, g := range groups {
		total := uptime.Sum(h.days(uptime.GroupKey(g.Name)))
		if total.Total() > 0 {
			sum += total.Uptime()
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

func (h *History) label() string {
	switch {
	case h == nil:
		return "uptime · not recorded"
	case h.Since.IsZero():
		return "uptime · collecting"
	case h.Since.After(time.Now().UTC().AddDate(0, 0, -UptimeDays)):
		return "uptime · since " + shortDate(h.Since)
	default:
		return fmt.Sprintf("uptime · %d days", UptimeDays)
	}
}

func (h *History) note() string {
	if h == nil {
		return "kami has nowhere to keep history; set KAMI_UPTIME_DB"
	}
	return "Share of the time kami saw each project not down. Degraded counts as up."
}

// formatPercent rounds down, so a day with any downtime never shows as 100%
func formatPercent(p float64) string {
	if p >= 1 {
		return "100%"
	}
	return fmt.Sprintf("%.2f%%", math.Floor(p*10000)/100)
}

// splitPercent separates the whole number from the rest, so they can be set at different sizes
func splitPercent(p float64, ok bool) (string, string, bool) {
	if !ok {
		return "", "", false
	}
	s := strings.TrimSuffix(formatPercent(p), "%")
	whole, frac, _ := strings.Cut(s, ".")
	if frac != "" {
		frac = "." + frac
	}
	return whole, frac + "%", true
}

func barColor(d uptime.Day) string {
	switch {
	case d.Total() == 0:
		return "bg-base-content/[0.06]"
	case d.Down == 0:
		return "bg-success/50"
	case d.Uptime() >= 0.99:
		return "bg-warning/70"
	default:
		return "bg-error/70"
	}
}

func percentColor(total uptime.Day) string {
	switch {
	case total.Total() == 0:
		return "text-base-content/20"
	case total.Down == 0:
		return "text-base-content/60"
	case total.Uptime() >= 0.99:
		return "text-warning/80"
	default:
		return "text-error/80"
	}
}

func barTitle(d uptime.Day) string {
	date := shortDate(d.Date)
	if d.Total() == 0 {
		return date + " · no data"
	}
	title := date + " · " + formatPercent(d.Uptime())
	if d.Down > 0 {
		title += " · " + shortDuration(time.Duration(d.Down)*time.Second) + " down"
	}
	return title
}

func shortDate(t time.Time) string {
	return strings.ToLower(t.Format("Jan 2"))
}

func shortDuration(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
}
