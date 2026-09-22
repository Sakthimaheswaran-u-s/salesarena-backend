// Package model holds the persisted documents and the API response shapes.
// JSON field names match what frontend/src/services/api.js expects.
package model

import "time"

// ---------- persisted documents ----------

type User struct {
	ID            string    `bson:"_id" json:"id"`
	Role          string    `bson:"role" json:"role"` // BDA | BDM
	Name          string    `bson:"name" json:"name"`
	Email         string    `bson:"email" json:"email"`
	PasswordHash  string    `bson:"passwordHash" json:"-"`
	Title         string    `bson:"title,omitempty" json:"title,omitempty"`
	Joined        string    `bson:"joined" json:"joined"`
	CurrentStreak int       `bson:"currentStreak" json:"-"`
	BestStreak    int       `bson:"bestStreak" json:"-"`
	LastLoginDate string    `bson:"lastLoginDate" json:"-"` // YYYY-MM-DD
	CreatedAt     time.Time `bson:"createdAt" json:"-"`
}

// Daily is the per-BDA, per-day rollup. _id is "<bdaId>:<date>".
type Daily struct {
	ID            string `bson:"_id,omitempty" json:"-"`
	BdaID         string `bson:"bdaId" json:"-"`
	Date          string `bson:"date" json:"date"`
	LoggedIn      bool   `bson:"loggedIn" json:"loggedIn"`
	StreakDay     int    `bson:"streakDay" json:"streakDay"`
	Calls         int    `bson:"calls" json:"calls"`
	CallMinutes   int    `bson:"callMinutes" json:"callMinutes"`
	LeadsWon      int    `bson:"leadsWon" json:"leadsWon"`
	LeadsDropped  int    `bson:"leadsDropped" json:"leadsDropped"`
	Points        int    `bson:"points" json:"points"`
	LoginPoints   int    `bson:"loginPoints" json:"-"`
	LeadPoints    int    `bson:"leadPoints" json:"-"`
	PenaltyPoints int    `bson:"penaltyPoints" json:"-"`
}

// Event is one point-changing action.
type Event struct {
	ID      string    `bson:"_id" json:"id"`
	BdaID   string    `bson:"bdaId" json:"-"`
	Date    string    `bson:"date" json:"-"`
	At      time.Time `bson:"at" json:"-"`
	Type    string    `bson:"type" json:"type"`
	Points  int       `bson:"points" json:"points"`
	Streak  int       `bson:"streak,omitempty" json:"streak,omitempty"`
	Company string    `bson:"company,omitempty" json:"company,omitempty"`
	Title   string    `bson:"title" json:"title"`
}

// ---------- API shapes ----------

// BdaRef is the public identity of an associate.
type BdaRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// Agg is a sum of Daily rollups over a window.
type Agg struct {
	Calls         int `bson:"calls" json:"calls"`
	CallMinutes   int `bson:"callMinutes" json:"callMinutes"`
	LeadsWon      int `bson:"leadsWon" json:"leadsWon"`
	LeadsDropped  int `bson:"leadsDropped" json:"leadsDropped"`
	Points        int `bson:"points" json:"points"`
	LoginPoints   int `bson:"loginPoints" json:"loginPoints"`
	LeadPoints    int `bson:"leadPoints" json:"leadPoints"`
	PenaltyPoints int `bson:"penaltyPoints" json:"penaltyPoints"`
	ActiveDays    int `bson:"activeDays" json:"activeDays"`
	LoginDays     int `bson:"loginDays" json:"loginDays"`
}

// Entry is one leaderboard row.
type Entry struct {
	Bda BdaRef `json:"bda"`
	Agg
	Streak        int      `json:"streak"`
	LoggedInToday bool     `json:"loggedInToday"`
	Rank          int      `json:"rank"`
	RankChange    *int     `json:"rankChange"`
	DropRate      *float64 `json:"dropRate,omitempty"`
}

// EventOut is Event with the timestamp formatted as local ISO (no offset),
// which is what the UI parses.
type EventOut struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Points  int    `json:"points"`
	Streak  int    `json:"streak,omitempty"`
	Company string `json:"company,omitempty"`
	At      string `json:"at"`
	Title   string `json:"title"`
}

type LoginDay struct {
	Date     string `json:"date"`
	LoggedIn bool   `json:"loggedIn"`
}

type Achievement struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Desc   string `json:"desc"`
	Earned bool   `json:"earned"`
}

type Reward struct {
	Streak     int  `json:"streak"`
	Points     int  `json:"points"`
	IsNew      bool `json:"isNew"`
	NextPoints int  `json:"nextPoints"`
}

type LoginResult struct {
	User   *User   `json:"user"`
	Reward *Reward `json:"reward"`
	Token  string  `json:"token"`
}

type Overview struct {
	Bda             BdaRef        `json:"bda"`
	Range           string        `json:"range"`
	Summary         *Entry        `json:"summary"`
	Rank            int           `json:"rank"`
	RankChange      *int          `json:"rankChange"`
	Total           int           `json:"total"`
	Above           *Entry        `json:"above"`
	Below           *Entry        `json:"below"`
	Leaders         []Entry       `json:"leaders"`
	Today           Daily         `json:"today"`
	Yesterday       Daily         `json:"yesterday"`
	Streak          int           `json:"streak"`
	BestStreak      int           `json:"bestStreak"`
	LoggedInToday   bool          `json:"loggedInToday"`
	NextLoginPoints int           `json:"nextLoginPoints"`
	Last7           []LoginDay    `json:"last7"`
	Daily           []Daily       `json:"daily"`
	Daily30         []Daily       `json:"daily30"`
	Feed            []EventOut    `json:"feed"`
	Achievements    []Achievement `json:"achievements"`
}

type DayWithEvents struct {
	Daily
	Events []EventOut `json:"events"`
}

type Activity struct {
	Bda     BdaRef          `json:"bda"`
	Days    []DayWithEvents `json:"days"`
	Daily30 []Daily         `json:"daily30"`
}

type Leaderboard struct {
	Range   string  `json:"range"`
	Total   int     `json:"total"`
	Entries []Entry `json:"entries"`
}

// DaySum is a whole-team rollup for one date.
type DaySum struct {
	Date         string `bson:"_id" json:"date"`
	Calls        int    `bson:"calls" json:"calls"`
	CallMinutes  int    `bson:"callMinutes" json:"callMinutes"`
	LeadsWon     int    `bson:"leadsWon" json:"leadsWon"`
	LeadsDropped int    `bson:"leadsDropped" json:"leadsDropped"`
	Points       int    `bson:"points" json:"points"`
	Active       int    `bson:"active" json:"active"`
	LoggedIn     int    `bson:"loggedIn" json:"loggedIn"`
}

type PeriodSum struct {
	Calls        int `json:"calls"`
	CallMinutes  int `json:"callMinutes"`
	LeadsWon     int `json:"leadsWon"`
	LeadsDropped int `json:"leadsDropped"`
	Points       int `json:"points"`
}

// Cycle is one three-month scoring period on the notice board.
type Cycle struct {
	ID           string  `json:"id"`
	Year         int     `json:"year"`
	Quarter      int     `json:"quarter"`
	From         string  `json:"from"`
	To           string  `json:"to"`
	EndsOn       string  `json:"endsOn"`
	Status       string  `json:"status"` // closed | running
	Days         int     `json:"days"`
	Participants int     `json:"participants"`
	Top          []Entry `json:"top"`
	Totals       Agg     `json:"totals"`
}

type NoticeYear struct {
	Year   int     `json:"year"`
	Cycles []Cycle `json:"cycles"`
}

type NoticeBoard struct {
	Years   []NoticeYear `json:"years"`
	Current *Cycle       `json:"current"`
}

// QuarterBoard is the standing of the cycle in progress.
type QuarterBoard struct {
	Year    int     `json:"year"`
	Quarter int     `json:"quarter"`
	EndsOn  string  `json:"endsOn"`
	Days    int     `json:"days"`
	Top     []Entry `json:"top"`
}

// RosterEntry is one row of the manager's associate list.
type RosterEntry struct {
	Bda           BdaRef `json:"bda"`
	Joined        string `json:"joined"`
	Streak        int    `json:"streak"`
	BestStreak    int    `json:"bestStreak"`
	LoggedInToday bool   `json:"loggedInToday"`
	Rank          int    `json:"rank"`
	Points        int    `json:"points"`
	Calls         int    `json:"calls"`
	LeadsWon      int    `json:"leadsWon"`
	LeadsDropped  int    `json:"leadsDropped"`
}

type Roster struct {
	Entries []RosterEntry `json:"entries"`
}

type TeamOverview struct {
	Range     string       `json:"range"`
	Today     DaySum       `json:"today"`
	Yesterday DaySum       `json:"yesterday"`
	Daily     []DaySum     `json:"daily"`
	Period    PeriodSum    `json:"period"`
	Top       []Entry      `json:"top"`
	Watchlist []Entry      `json:"watchlist"`
	Quarter   QuarterBoard `json:"quarter"`
	TotalBdas int          `json:"totalBdas"`
}

type ReportRow struct {
	Bda BdaRef `json:"bda"`
	Daily
}

type DailyReport struct {
	Date      string      `json:"date"`
	Rows      []ReportRow `json:"rows"`
	Totals    *DaySum     `json:"totals"`
	Available bool        `json:"available"`
	MinDate   string      `json:"minDate,omitempty"`
}
