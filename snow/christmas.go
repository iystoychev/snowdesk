package snow

import (
	"fmt"
	"math"
	"time"
)

// DaysToChristmas counts calendar days from now's date to the next Dec 25
// (0 on Christmas Day), in now's time zone.
func DaysToChristmas(now time.Time) int {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	xmas := time.Date(now.Year(), time.December, 25, 0, 0, 0, 0, loc)
	if today.After(xmas) {
		xmas = xmas.AddDate(1, 0, 0)
	}
	// Round: days around a DST change are 23 or 25 hours long.
	return int(math.Round(xmas.Sub(today).Hours() / 24))
}

// CountdownTitle is the menu bar title: "❄︎ 51d", or a tree on Christmas Day.
func CountdownTitle(now time.Time) string {
	if d := DaysToChristmas(now); d > 0 {
		return fmt.Sprintf("❄︎ %dd", d)
	}
	return "❄︎ 🎄"
}
