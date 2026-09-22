// Package seed generates the demo dataset. It is a faithful port of the
// generator in frontend/src/services/mockApi.js (same hash, same PRNG, same
// draw order) so the numbers match the frontend's standalone mock.
package seed

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/salesarena/backend/internal/model"
	"github.com/salesarena/backend/internal/rules"
	"github.com/salesarena/backend/internal/store"
)

// 16 months, so the notice board opens with several closed quarters.
const HistoryDays = 480

type demoUser struct {
	ID, Role, Name, Email, Password, Title string
	Skill                                  float64
}

var DemoUsers = []demoUser{
	{ID: "bdm-1", Role: "BDM", Name: "Rahul Verma", Email: "rahul.verma@salesarena.io", Password: "bdm123", Title: "Business Development Manager"},
	{ID: "bda-1", Role: "BDA", Name: "Priya Sharma", Email: "priya.sharma@salesarena.io", Password: "bda123", Skill: 1.2},
	{ID: "bda-2", Role: "BDA", Name: "Arjun Mehta", Email: "arjun.mehta@salesarena.io", Password: "bda123", Skill: 1.3},
	{ID: "bda-3", Role: "BDA", Name: "Sneha Iyer", Email: "sneha.iyer@salesarena.io", Password: "bda123", Skill: 1.05},
	{ID: "bda-4", Role: "BDA", Name: "Karthik Raj", Email: "karthik.raj@salesarena.io", Password: "bda123", Skill: 0.95},
	{ID: "bda-5", Role: "BDA", Name: "Ananya Das", Email: "ananya.das@salesarena.io", Password: "bda123", Skill: 1.1},
	{ID: "bda-6", Role: "BDA", Name: "Vikram Singh", Email: "vikram.singh@salesarena.io", Password: "bda123", Skill: 0.8},
	{ID: "bda-7", Role: "BDA", Name: "Meera Nair", Email: "meera.nair@salesarena.io", Password: "bda123", Skill: 1.0},
	{ID: "bda-8", Role: "BDA", Name: "Rohan Kapoor", Email: "rohan.kapoor@salesarena.io", Password: "bda123", Skill: 0.7},
	{ID: "bda-9", Role: "BDA", Name: "Divya Menon", Email: "divya.menon@salesarena.io", Password: "bda123", Skill: 1.15},
	{ID: "bda-10", Role: "BDA", Name: "Aditya Rao", Email: "aditya.rao@salesarena.io", Password: "bda123", Skill: 0.85},
	{ID: "bda-11", Role: "BDA", Name: "Kavya Reddy", Email: "kavya.reddy@salesarena.io", Password: "bda123", Skill: 0.9},
	{ID: "bda-12", Role: "BDA", Name: "Nikhil Bose", Email: "nikhil.bose@salesarena.io", Password: "bda123", Skill: 0.75},
}

var companies = []string{
	"Acme Corp", "Globex", "Initech", "Umbrella Health", "Stark Industries", "Wayne Enterprises",
	"Hooli", "Pied Piper", "Vandelay Imports", "Soylent Foods", "Wonka Industries", "Cyberdyne Systems",
	"Massive Dynamic", "Bluth Company", "Dunder Mifflin", "Prestige Worldwide", "Oscorp", "Tyrell Corp",
	"Aperture Labs", "Gekko & Co", "Sterling Cooper", "Nakatomi Trading", "Virtucon", "Genco Olive Oil",
}

// ---------- deterministic PRNG (matches the JS implementation bit for bit) ----------

func hashSeed(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

type prng struct{ a uint32 }

func (r *prng) next() float64 {
	r.a += 0x6d2b79f5
	t := (r.a ^ (r.a >> 15)) * (1 | r.a)
	t = (t + (t^(t>>7))*(61|t)) ^ t
	return float64(t^(t>>14)) / 4294967296
}

// ---------- generation ----------

type rawDay struct {
	loggedIn     bool
	calls        int
	callMinutes  int
	won, dropped []string
}

func jsRound(f float64) int {
	// JS Math.round rounds .5 toward +inf, Go math.Round rounds away from zero.
	return int(floor(f + 0.5))
}

func floor(f float64) float64 {
	i := float64(int64(f))
	if f < i {
		return i - 1
	}
	return i
}

func generateRaw(u demoUser, dates []string, loc *time.Location) map[string]rawDay {
	r := &prng{a: hashSeed(u.ID)}
	pick := func() string { return companies[int(r.next()*float64(len(companies)))] }
	out := make(map[string]rawDay, len(dates))
	for idx, key := range dates {
		d, _ := time.ParseInLocation("2006-01-02", key, loc)
		weekend := d.Weekday() == time.Saturday || d.Weekday() == time.Sunday
		isToday := idx == len(dates)-1
		var active bool
		if isToday {
			active = true
		} else {
			p := 0.93
			if weekend {
				p = 0.2
			}
			active = r.next() < p
		}
		weekendLogin := r.next() < 0.6
		rr := [5]float64{r.next(), r.next(), r.next(), r.next(), r.next()}
		day := rawDay{}
		if active {
			scale := 1.0
			if isToday {
				scale = 0.55
			}
			base := 15.0
			if weekend {
				base = 6
			}
			calls := jsRound((base + (rr[0]-0.5)*10) * u.Skill * scale)
			if calls < 0 {
				calls = 0
			}
			day.calls = calls
			day.callMinutes = jsRound(float64(calls) * (3.5 + rr[1]*4))
			wonN := int(floor(rr[2] * 3.4 * u.Skill * scale))
			if wonN > 5 {
				wonN = 5
			}
			dropN := 0
			if rr[3] < 0.28 {
				dropN = 1
				if rr[4] < 0.25 {
					dropN = 2
				}
			}
			for i := 0; i < wonN; i++ {
				day.won = append(day.won, pick())
			}
			for i := 0; i < dropN; i++ {
				day.dropped = append(day.dropped, pick())
			}
		}
		// Nobody has logged in to the portal today until they actually do.
		day.loggedIn = !isToday && (active || (weekend && weekendLogin))
		out[key] = day
	}
	return out
}

// Run seeds users, rollups and events for the last HistoryDays days.
func Run(ctx context.Context, st *store.Store, now time.Time) error {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dates := make([]string, 0, HistoryDays)
	for i := HistoryDays - 1; i >= 0; i-- {
		dates = append(dates, today.AddDate(0, 0, -i).Format("2006-01-02"))
	}

	var users []model.User
	var dailies []model.Daily
	var events []model.Event

	for _, du := range DemoUsers {
		hash, err := bcrypt.GenerateFromPassword([]byte(du.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		user := model.User{
			ID: du.ID, Role: du.Role, Name: du.Name, Email: du.Email, PasswordHash: string(hash),
			Title: du.Title, Joined: dates[0], CreatedAt: now,
		}
		if du.Role != "BDA" {
			users = append(users, user)
			continue
		}

		raw := generateRaw(du, dates, loc)
		streak, best, lastLogin, lastStreak := 0, 0, "", 0
		for _, key := range dates {
			rd := raw[key]
			if rd.loggedIn {
				streak++
			} else {
				streak = 0
			}
			day := model.Daily{
				BdaID: du.ID, Date: key, LoggedIn: rd.loggedIn, Calls: rd.calls, CallMinutes: rd.callMinutes,
				LeadsWon: len(rd.won), LeadsDropped: len(rd.dropped),
			}
			dayStart, _ := time.ParseInLocation("2006-01-02", key, loc)
			if rd.loggedIn {
				pts := rules.LoginPoints(streak)
				day.StreakDay = streak
				day.LoginPoints = pts
				events = append(events, model.Event{
					ID: fmt.Sprintf("%s-%s-login", du.ID, key), BdaID: du.ID, Date: key,
					At: dayStart.Add(9*time.Hour + 5*time.Minute), Type: rules.EventLogin, Points: pts, Streak: streak,
					Title: fmt.Sprintf("Daily login · streak day %d", streak),
				})
				lastLogin, lastStreak = key, streak
				if streak > best {
					best = streak
				}
			}
			for i, c := range rd.won {
				day.LeadPoints += rules.LeadWon
				events = append(events, model.Event{
					ID: fmt.Sprintf("%s-%s-won-%d", du.ID, key, i), BdaID: du.ID, Date: key,
					At:   dayStart.Add(10*time.Hour + 30*time.Minute + time.Duration(i*95)*time.Minute),
					Type: rules.EventLeadWon, Points: rules.LeadWon, Company: c, Title: "Lead won · " + c,
				})
			}
			for i, c := range rd.dropped {
				day.PenaltyPoints += rules.LeadDropped
				events = append(events, model.Event{
					ID: fmt.Sprintf("%s-%s-drop-%d", du.ID, key, i), BdaID: du.ID, Date: key,
					At:   dayStart.Add(14*time.Hour + 15*time.Minute + time.Duration(i*70)*time.Minute),
					Type: rules.EventLeadDropped, Points: rules.LeadDropped, Company: c, Title: "Lead dropped · " + c,
				})
			}
			day.Points = day.LoginPoints + day.LeadPoints + day.PenaltyPoints
			dailies = append(dailies, day)
		}
		user.CurrentStreak, user.BestStreak, user.LastLoginDate = lastStreak, best, lastLogin
		users = append(users, user)
	}

	if err := st.InsertUsers(ctx, users); err != nil {
		return fmt.Errorf("seed users: %w", err)
	}
	if err := st.InsertDaily(ctx, dailies); err != nil {
		return fmt.Errorf("seed daily: %w", err)
	}
	if err := st.InsertEvents(ctx, events); err != nil {
		return fmt.Errorf("seed events: %w", err)
	}
	slog.Info("seeded demo data", "users", len(users), "days", len(dailies), "events", len(events))
	return nil
}
