// Package rules is the single source of truth for scoring.
// It mirrors frontend/src/config/rules.js exactly.
package rules

const (
	LeadWon           = 50  // portal points for each successful lead
	LeadDropped       = -50 // portal points for each dropped lead
	LoginBase         = 10  // points for logging in on any day
	StreakBonusPerDay = 5   // extra points for every consecutive login day
	StreakBonusCap    = 50  // the streak bonus stops growing here
)

// LoginPoints returns the points awarded for the login on day N of a streak.
func LoginPoints(streakDay int) int {
	if streakDay < 1 {
		streakDay = 1
	}
	bonus := (streakDay - 1) * StreakBonusPerDay
	if bonus > StreakBonusCap {
		bonus = StreakBonusCap
	}
	return LoginBase + bonus
}

var StreakMilestones = []int{3, 7, 14, 30}

// Range is a leaderboard time window. Days == 0 means "all time".
type Range struct {
	Key   string
	Label string
	Days  int
}

var Ranges = []Range{
	{Key: "today", Label: "Today", Days: 1},
	{Key: "week", Label: "7 days", Days: 7},
	{Key: "month", Label: "30 days", Days: 30},
	{Key: "all", Label: "All time", Days: 0},
}

// ParseRange returns the range for key, defaulting to "month".
func ParseRange(key string) Range {
	for _, r := range Ranges {
		if r.Key == key {
			return r
		}
	}
	return Ranges[2]
}

// Event types
const (
	EventLogin       = "LOGIN"
	EventLeadWon     = "LEAD_WON"
	EventLeadDropped = "LEAD_DROPPED"
)
