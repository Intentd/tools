package condition

import (
	"testing"
	"time"
)

// A condition with time and dayOfWeek holds only when both do: a window on
// those days, not whichever field is checked first.
func TestClockHolds_TimeAndDayOfWeek(t *testing.T) {
	yes := true
	c := Condition{Time: &TimeWindow{After: "09:00", Before: "17:00"}, DayOfWeek: &DayOfWeek{Weekday: &yes}}
	monday10 := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	saturday10 := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	monday20 := time.Date(2026, 11, 23, 20, 0, 0, 0, time.UTC)
	if !clockHolds(nil, c, monday10) {
		t.Error("Monday 10:00: want the window open")
	}
	if clockHolds(nil, c, saturday10) {
		t.Error("Saturday 10:00: inside the hours, but not a weekday")
	}
	if clockHolds(nil, c, monday20) {
		t.Error("Monday 20:00: a weekday, but outside the hours")
	}
}
