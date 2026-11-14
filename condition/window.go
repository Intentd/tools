package condition

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// TimeWindow declares a clock-based active window.
type TimeWindow struct {
	// After — active after this time (format: "HH:MM" in 24h).
	After string `yaml:"after,omitempty" json:"after,omitempty"`

	// Before — active before this time (format: "HH:MM" in 24h).
	Before string `yaml:"before,omitempty" json:"before,omitempty"`
}

// DayOfWeek declares which days the condition is active.
// Exactly one of Weekday, Weekend, In, or NotIn must be set.
type DayOfWeek struct {
	// Weekday — active Mon–Fri. Shorthand for in: [Monday..Friday].
	Weekday *bool `yaml:"weekday,omitempty" json:"weekday,omitempty"`

	// Weekend — active Sat–Sun. Shorthand for in: [Saturday, Sunday].
	Weekend *bool `yaml:"weekend,omitempty" json:"weekend,omitempty"`

	// In — active on these days. Full English names: Monday, Tuesday, etc.
	In []string `yaml:"in,omitempty" json:"in,omitempty"`

	// NotIn — active on all days except these.
	NotIn []string `yaml:"notIn,omitempty" json:"notIn,omitempty"`
}

// evalTimeWindow reports whether now is within the HH:MM window.
func evalTimeWindow(tw *TimeWindow, now time.Time) bool {
	if tw.After != "" {
		t, err := parseHHMM(tw.After, now)
		if err != nil || now.Before(t) {
			return false
		}
	}
	if tw.Before != "" {
		t, err := parseHHMM(tw.Before, now)
		if err != nil || now.After(t) {
			return false
		}
	}
	return true
}

// DayOfWeekAt reports whether now's day matches d.
func DayOfWeekAt(d *DayOfWeek, now time.Time) bool {
	wd := now.Weekday()
	if d.Weekday != nil {
		isWeekday := wd >= time.Monday && wd <= time.Friday
		return *d.Weekday == isWeekday
	}
	if d.Weekend != nil {
		isWeekend := wd == time.Saturday || wd == time.Sunday
		return *d.Weekend == isWeekend
	}
	today := wd.String()
	if len(d.In) > 0 {
		for _, day := range d.In {
			if strings.EqualFold(day, today) {
				return true
			}
		}
		return false
	}
	if len(d.NotIn) > 0 {
		for _, day := range d.NotIn {
			if strings.EqualFold(day, today) {
				return false
			}
		}
		return true
	}
	return false
}

// evalCronWindow reports whether a cron window is open: it opens at each fire
// and stays open for duration. A zero duration is one period of the schedule
// (the gap between two consecutive fires), so the window stays open from one
// fire to the next.
func evalCronWindow(cronExpr string, duration time.Duration, now time.Time) bool {
	schedule, err := cron.ParseStandard(cronExpr)
	if err != nil {
		return false
	}
	if duration == 0 {
		next := schedule.Next(now)
		duration = schedule.Next(next).Sub(next)
	}
	prev := schedule.Next(now.Add(-duration))
	return !prev.After(now)
}

// parseHHMM parses "HH:MM" as that time today in now's location.
func parseHHMM(s string, now time.Time) (time.Time, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time %q: expected HH:MM", s)
	}
	return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location()), nil
}
