// Package service implements the portal's business logic on top of the
// Mongo store and Redis cache.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"

	"github.com/salesarena/backend/internal/auth"
	"github.com/salesarena/backend/internal/cache"
	"github.com/salesarena/backend/internal/model"
	"github.com/salesarena/backend/internal/rules"
	"github.com/salesarena/backend/internal/store"
)

const (
	dateLayout   = "2006-01-02"
	localISO     = "2006-01-02T15:04:05"
	readCacheTTL = 30 * time.Second
	historyDays  = 60
)

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrWrongRole          = errors.New("wrong role")
	ErrNotFound           = errors.New("not found")
	ErrBadInput           = errors.New("bad input")
)

type Service struct {
	store      *store.Store
	cache      *cache.Cache
	loc        *time.Location
	sessionTTL time.Duration
	Now        func() time.Time
}

func New(st *store.Store, c *cache.Cache, loc *time.Location, sessionTTL time.Duration) *Service {
	return &Service{store: st, cache: c, loc: loc, sessionTTL: sessionTTL, Now: time.Now}
}

// ---------- date helpers ----------

func (s *Service) now() time.Time { return s.Now().In(s.loc) }

func (s *Service) today() time.Time {
	n := s.now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, s.loc)
}

func key(t time.Time) string { return t.Format(dateLayout) }

// window returns [from, to] date keys for a range, shifted back `offset`
// windows. For "all", from is "" (unbounded) and offset>0 yields no window.
func (s *Service) window(r rules.Range, offset int) (from, to string, ok bool) {
	today := s.today()
	if r.Days == 0 {
		if offset > 0 {
			return "", "", false
		}
		return "", key(today), true
	}
	end := today.AddDate(0, 0, -offset*r.Days)
	start := end.AddDate(0, 0, -(r.Days - 1))
	return key(start), key(end), true
}

// lastNDates returns date keys for the last n days ending today, ascending.
func (s *Service) lastNDates(n int) []string {
	today := s.today()
	out := make([]string, 0, n)
	for i := n - 1; i >= 0; i-- {
		out = append(out, key(today.AddDate(0, 0, -i)))
	}
	return out
}

// ---------- shared shaping ----------

func ref(u model.User) model.BdaRef {
	return model.BdaRef{ID: u.ID, Name: u.Name, Email: u.Email, Role: u.Role}
}

// currentStreak applies the "alive until end of next day" rule.
func (s *Service) currentStreak(u model.User) (streak int, loggedInToday bool) {
	today := key(s.today())
	yesterday := key(s.today().AddDate(0, 0, -1))
	switch u.LastLoginDate {
	case today:
		return u.CurrentStreak, true
	case yesterday:
		return u.CurrentStreak, false
	default:
		return 0, false
	}
}

func rank(users []model.User, aggs map[string]model.Agg, streakOf func(model.User) (int, bool)) []model.Entry {
	entries := make([]model.Entry, 0, len(users))
	for _, u := range users {
		st, today := streakOf(u)
		entries = append(entries, model.Entry{Bda: ref(u), Agg: aggs[u.ID], Streak: st, LoggedInToday: today})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if a.LeadsWon != b.LeadsWon {
			return a.LeadsWon > b.LeadsWon
		}
		if a.LeadsDropped != b.LeadsDropped {
			return a.LeadsDropped < b.LeadsDropped
		}
		if a.Calls != b.Calls {
			return a.Calls > b.Calls
		}
		return a.Bda.Name < b.Bda.Name
	})
	for i := range entries {
		entries[i].Rank = i + 1
	}
	return entries
}

// board ranks every BDA for a range, with rank change vs the previous window.
func (s *Service) board(ctx context.Context, users []model.User, r rules.Range) ([]model.Entry, error) {
	from, to, _ := s.window(r, 0)
	aggs, err := s.store.AggregateByBDA(ctx, from, to)
	if err != nil {
		return nil, err
	}
	entries := rank(users, aggs, s.currentStreak)

	if pf, pt, ok := s.window(r, 1); ok {
		prevAggs, err := s.store.AggregateByBDA(ctx, pf, pt)
		if err != nil {
			return nil, err
		}
		prev := rank(users, prevAggs, s.currentStreak)
		prevRank := make(map[string]int, len(prev))
		for _, e := range prev {
			prevRank[e.Bda.ID] = e.Rank
		}
		for i := range entries {
			ch := prevRank[entries[i].Bda.ID] - entries[i].Rank
			entries[i].RankChange = &ch
		}
	}
	return entries, nil
}

// fillDays returns one Daily per date, substituting zero rows for gaps.
func fillDays(bdaID string, dates []string, docs []model.Daily) []model.Daily {
	byDate := make(map[string]model.Daily, len(docs))
	for _, d := range docs {
		byDate[d.Date] = d
	}
	out := make([]model.Daily, 0, len(dates))
	for _, dt := range dates {
		if d, ok := byDate[dt]; ok {
			out = append(out, d)
		} else {
			out = append(out, model.Daily{BdaID: bdaID, Date: dt})
		}
	}
	return out
}

func fillDaySums(dates []string, rows []model.DaySum) []model.DaySum {
	byDate := make(map[string]model.DaySum, len(rows))
	for _, r := range rows {
		byDate[r.Date] = r
	}
	out := make([]model.DaySum, 0, len(dates))
	for _, dt := range dates {
		if d, ok := byDate[dt]; ok {
			out = append(out, d)
		} else {
			out = append(out, model.DaySum{Date: dt})
		}
	}
	return out
}

func (s *Service) eventOut(e model.Event) model.EventOut {
	return model.EventOut{
		ID: e.ID, Type: e.Type, Points: e.Points, Streak: e.Streak, Company: e.Company,
		At: e.At.In(s.loc).Format(localISO), Title: e.Title,
	}
}

// ---------- auth ----------

func (s *Service) Login(ctx context.Context, email, password, role string) (*model.LoginResult, error) {
	u, err := s.store.UserByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, store.ErrNotFound) || (err == nil && !auth.CheckPassword(u.PasswordHash, password)) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if u.Role != role {
		return nil, fmt.Errorf("%w: %s", ErrWrongRole, u.Role)
	}

	var reward *model.Reward
	if u.Role == "BDA" {
		if reward, err = s.recordLogin(ctx, u); err != nil {
			return nil, err
		}
	}

	token, err := auth.NewToken()
	if err != nil {
		return nil, err
	}
	sess := cache.Session{UserID: u.ID, Role: u.Role, CreatedAt: s.now()}
	if err := s.cache.SaveSession(ctx, token, sess, s.sessionTTL); err != nil {
		return nil, err
	}
	return &model.LoginResult{User: u, Reward: reward, Token: token}, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	return s.cache.DeleteSession(ctx, token)
}

func (s *Service) Authenticate(ctx context.Context, token string) (*cache.Session, error) {
	return s.cache.GetSession(ctx, token, s.sessionTTL)
}

func (s *Service) UserByID(ctx context.Context, id string) (*model.User, error) {
	u, err := s.store.UserByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrNotFound
	}
	return u, err
}

// recordLogin awards today's login points exactly once per BDA per day.
func (s *Service) recordLogin(ctx context.Context, u *model.User) (*model.Reward, error) {
	today := key(s.today())
	yesterday := key(s.today().AddDate(0, 0, -1))

	notNew := func(u *model.User) *model.Reward {
		return &model.Reward{Streak: u.CurrentStreak, Points: rules.LoginPoints(u.CurrentStreak), IsNew: false, NextPoints: rules.LoginPoints(u.CurrentStreak + 1)}
	}
	if u.LastLoginDate == today {
		return notNew(u), nil
	}

	first, err := s.cache.AcquireLoginLock(ctx, u.ID, today)
	if err != nil {
		return nil, err
	}
	if !first {
		// A concurrent request already awarded today; re-read for the streak.
		fresh, err := s.store.UserByID(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		*u = *fresh
		return notNew(u), nil
	}

	streak := 1
	if u.LastLoginDate == yesterday {
		streak = u.CurrentStreak + 1
	}
	best := u.BestStreak
	if streak > best {
		best = streak
	}
	pts := rules.LoginPoints(streak)
	ev := model.Event{
		ID: fmt.Sprintf("%s-%s-login", u.ID, today), BdaID: u.ID, Date: today, At: s.now(),
		Type: rules.EventLogin, Points: pts, Streak: streak, Title: fmt.Sprintf("Daily login · streak day %d", streak),
	}
	if err := s.store.InsertEvent(ctx, ev); err != nil {
		_ = s.cache.ReleaseLoginLock(ctx, u.ID, today)
		return nil, err
	}
	if err := s.store.UpsertDaily(ctx, u.ID, today,
		bson.M{"points": pts, "loginPoints": pts},
		bson.M{"loggedIn": true, "streakDay": streak}); err != nil {
		return nil, err
	}
	if err := s.store.UpdateUserStreak(ctx, u.ID, streak, best, today); err != nil {
		return nil, err
	}
	u.CurrentStreak, u.BestStreak, u.LastLoginDate = streak, best, today
	_ = s.cache.Invalidate(ctx)
	return &model.Reward{Streak: streak, Points: pts, IsNew: true, NextPoints: rules.LoginPoints(streak + 1)}, nil
}

// ---------- activity ingestion ----------

// RecordCalls adds calls and talk-time minutes to a BDA's day.
func (s *Service) RecordCalls(ctx context.Context, bdaID, date string, calls, minutes int) (*model.Daily, error) {
	if calls < 0 || minutes < 0 || (calls == 0 && minutes == 0) {
		return nil, fmt.Errorf("%w: calls and minutes must be >= 0 and not both zero", ErrBadInput)
	}
	if _, err := s.mustBDA(ctx, bdaID); err != nil {
		return nil, err
	}
	date, err := s.normalizeDate(date)
	if err != nil {
		return nil, err
	}
	if err := s.store.UpsertDaily(ctx, bdaID, date, bson.M{"calls": calls, "callMinutes": minutes}, nil); err != nil {
		return nil, err
	}
	_ = s.cache.Invalidate(ctx)
	d, err := s.store.DailyOne(ctx, bdaID, date)
	return d, err
}

// RecordLead logs a won or dropped lead and applies its points.
func (s *Service) RecordLead(ctx context.Context, bdaID, date, outcome, company string) (*model.EventOut, error) {
	outcome = strings.ToLower(strings.TrimSpace(outcome))
	company = strings.TrimSpace(company)
	if company == "" {
		company = "Unnamed lead"
	}
	var typ, title string
	var pts int
	var incField string
	switch outcome {
	case "won":
		typ, pts, title, incField = rules.EventLeadWon, rules.LeadWon, "Lead won · "+company, "leadsWon"
	case "dropped":
		typ, pts, title, incField = rules.EventLeadDropped, rules.LeadDropped, "Lead dropped · "+company, "leadsDropped"
	default:
		return nil, fmt.Errorf("%w: outcome must be 'won' or 'dropped'", ErrBadInput)
	}
	if _, err := s.mustBDA(ctx, bdaID); err != nil {
		return nil, err
	}
	date, err := s.normalizeDate(date)
	if err != nil {
		return nil, err
	}
	at := s.now()
	if date != key(s.today()) {
		d, _ := time.ParseInLocation(dateLayout, date, s.loc)
		at = d.Add(12 * time.Hour)
	}
	ev := model.Event{
		ID: fmt.Sprintf("%s-%s-%s-%d", bdaID, date, outcome, at.UnixNano()), BdaID: bdaID, Date: date, At: at,
		Type: typ, Points: pts, Company: company, Title: title,
	}
	if err := s.store.InsertEvent(ctx, ev); err != nil {
		return nil, err
	}
	inc := bson.M{incField: 1, "points": pts}
	if pts > 0 {
		inc["leadPoints"] = pts
	} else {
		inc["penaltyPoints"] = pts
	}
	if err := s.store.UpsertDaily(ctx, bdaID, date, inc, nil); err != nil {
		return nil, err
	}
	_ = s.cache.Invalidate(ctx)
	out := s.eventOut(ev)
	return &out, nil
}

func (s *Service) mustBDA(ctx context.Context, id string) (*model.User, error) {
	u, err := s.UserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if u.Role != "BDA" {
		return nil, ErrNotFound
	}
	return u, nil
}

func (s *Service) normalizeDate(date string) (string, error) {
	if date == "" {
		return key(s.today()), nil
	}
	t, err := time.ParseInLocation(dateLayout, date, s.loc)
	if err != nil {
		return "", fmt.Errorf("%w: date must be YYYY-MM-DD", ErrBadInput)
	}
	if t.After(s.today()) {
		return "", fmt.Errorf("%w: date cannot be in the future", ErrBadInput)
	}
	return key(t), nil
}

// ---------- BDA reads ----------

func (s *Service) Overview(ctx context.Context, bdaID, rangeKey string) (*model.Overview, error) {
	r := rules.ParseRange(rangeKey)
	cacheKey := "overview:" + bdaID + ":" + r.Key
	var cached model.Overview
	if s.cache.GetJSON(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	me, err := s.mustBDA(ctx, bdaID)
	if err != nil {
		return nil, err
	}
	users, err := s.store.ListBDAs(ctx)
	if err != nil {
		return nil, err
	}
	board, err := s.board(ctx, users, r)
	if err != nil {
		return nil, err
	}
	var mine *model.Entry
	for i := range board {
		if board[i].Bda.ID == bdaID {
			mine = &board[i]
			break
		}
	}
	if mine == nil {
		return nil, ErrNotFound
	}

	// month rank + all-time aggregate for achievements
	mf, mt, _ := s.window(rules.ParseRange("month"), 0)
	monthAggs, err := s.store.AggregateByBDA(ctx, mf, mt)
	if err != nil {
		return nil, err
	}
	monthRank := 0
	for _, e := range rank(users, monthAggs, s.currentStreak) {
		if e.Bda.ID == bdaID {
			monthRank = e.Rank
		}
	}
	allAggs, err := s.store.AggregateByBDA(ctx, "", key(s.today()))
	if err != nil {
		return nil, err
	}

	dates30 := s.lastNDates(30)
	docs, err := s.store.DailyRange(ctx, bdaID, dates30[0], dates30[len(dates30)-1])
	if err != nil {
		return nil, err
	}
	daily30 := fillDays(bdaID, dates30, docs)
	daily14 := daily30[len(daily30)-14:]
	last7 := make([]model.LoginDay, 0, 7)
	for _, d := range daily30[len(daily30)-7:] {
		last7 = append(last7, model.LoginDay{Date: d.Date, LoggedIn: d.LoggedIn})
	}
	today := daily30[len(daily30)-1]
	yesterday := daily30[len(daily30)-2]

	events, err := s.store.EventsByBDA(ctx, bdaID, 12)
	if err != nil {
		return nil, err
	}
	feed := make([]model.EventOut, 0, len(events))
	for _, e := range events {
		feed = append(feed, s.eventOut(e))
	}

	streak, loggedInToday := s.currentStreak(*me)
	var above, below *model.Entry
	if mine.Rank >= 2 {
		above = &board[mine.Rank-2]
	}
	if mine.Rank < len(board) {
		below = &board[mine.Rank]
	}
	leaders := board
	if len(leaders) > 5 {
		leaders = leaders[:5]
	}

	out := &model.Overview{
		Bda: ref(*me), Range: r.Key, Summary: mine, Rank: mine.Rank, RankChange: mine.RankChange, Total: len(board),
		Above: above, Below: below, Leaders: leaders, Today: today, Yesterday: yesterday,
		Streak: streak, BestStreak: me.BestStreak, LoggedInToday: loggedInToday,
		NextLoginPoints: rules.LoginPoints(streak + 1), Last7: last7, Daily: daily14, Daily30: daily30, Feed: feed,
		Achievements: achievements(me.BestStreak, monthRank, allAggs[bdaID], daily30[len(daily30)-7:]),
	}
	s.cache.SetJSON(ctx, cacheKey, out, readCacheTTL)
	return out, nil
}

func achievements(bestStreak, monthRank int, allTime model.Agg, last7 []model.Daily) []model.Achievement {
	cleanWeek := true
	anyWon := false
	for _, d := range last7 {
		if d.LeadsDropped > 0 {
			cleanWeek = false
		}
		if d.LeadsWon > 0 {
			anyWon = true
		}
	}
	return []model.Achievement{
		{ID: "streak-3", Title: "Warm-up", Desc: "3-day login streak", Earned: bestStreak >= 3},
		{ID: "streak-7", Title: "On fire", Desc: "7-day login streak", Earned: bestStreak >= 7},
		{ID: "streak-14", Title: "Unstoppable", Desc: "14-day login streak", Earned: bestStreak >= 14},
		{ID: "closer-10", Title: "Closer", Desc: "10 leads won", Earned: allTime.LeadsWon >= 10},
		{ID: "closer-50", Title: "Rainmaker", Desc: "50 leads won", Earned: allTime.LeadsWon >= 50},
		{ID: "dialer-500", Title: "Dialer", Desc: "500 calls made", Earned: allTime.Calls >= 500},
		{ID: "podium", Title: "Podium", Desc: "Top 3 this month", Earned: monthRank > 0 && monthRank <= 3},
		{ID: "clean-week", Title: "Clean sheet", Desc: "A week with no drops", Earned: cleanWeek && anyWon},
	}
}

func (s *Service) Activity(ctx context.Context, bdaID string) (*model.Activity, error) {
	me, err := s.mustBDA(ctx, bdaID)
	if err != nil {
		return nil, err
	}
	docs, err := s.store.DailyRange(ctx, bdaID, "", key(s.today()))
	if err != nil {
		return nil, err
	}
	events, err := s.store.EventsByBDA(ctx, bdaID, 0)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string][]model.EventOut)
	for _, e := range events { // already newest first
		byDate[e.Date] = append(byDate[e.Date], s.eventOut(e))
	}
	days := make([]model.DayWithEvents, 0, len(docs))
	for i := len(docs) - 1; i >= 0; i-- {
		d := docs[i]
		evs := byDate[d.Date]
		if len(evs) == 0 && d.Calls == 0 {
			continue
		}
		if evs == nil {
			evs = []model.EventOut{}
		}
		days = append(days, model.DayWithEvents{Daily: d, Events: evs})
	}
	dates30 := s.lastNDates(30)
	return &model.Activity{Bda: ref(*me), Days: days, Daily30: fillDays(bdaID, dates30, docs)}, nil
}

// ---------- leaderboard ----------

func (s *Service) Leaderboard(ctx context.Context, rangeKey, search string) (*model.Leaderboard, error) {
	r := rules.ParseRange(rangeKey)
	var entries []model.Entry
	cacheKey := "board:" + r.Key
	if !s.cache.GetJSON(ctx, cacheKey, &entries) {
		users, err := s.store.ListBDAs(ctx)
		if err != nil {
			return nil, err
		}
		if entries, err = s.board(ctx, users, r); err != nil {
			return nil, err
		}
		s.cache.SetJSON(ctx, cacheKey, entries, readCacheTTL)
	}
	total := len(entries)
	q := strings.ToLower(strings.TrimSpace(search))
	filtered := make([]model.Entry, 0, len(entries))
	for _, e := range entries {
		if q != "" && !strings.Contains(strings.ToLower(e.Bda.Name), q) &&
			!strings.Contains(strings.ToLower(e.Bda.Email), q) {
			continue
		}
		filtered = append(filtered, e)
	}
	return &model.Leaderboard{Range: r.Key, Total: total, Entries: filtered}, nil
}

// ---------- BDM reads ----------

func (s *Service) TeamOverview(ctx context.Context, rangeKey string) (*model.TeamOverview, error) {
	r := rules.ParseRange(rangeKey)
	cacheKey := "team:" + r.Key
	var cached model.TeamOverview
	if s.cache.GetJSON(ctx, cacheKey, &cached) {
		return &cached, nil
	}

	users, err := s.store.ListBDAs(ctx)
	if err != nil {
		return nil, err
	}
	dates14 := s.lastNDates(14)
	rows, err := s.store.AggregateByDate(ctx, dates14[0], dates14[len(dates14)-1])
	if err != nil {
		return nil, err
	}
	daily := fillDaySums(dates14, rows)

	from, to, _ := s.window(r, 0)
	periodRows, err := s.store.AggregateByDate(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var period model.PeriodSum
	for _, d := range periodRows {
		period.Calls += d.Calls
		period.CallMinutes += d.CallMinutes
		period.LeadsWon += d.LeadsWon
		period.LeadsDropped += d.LeadsDropped
		period.Points += d.Points
	}

	board, err := s.board(ctx, users, r)
	if err != nil {
		return nil, err
	}
	top := board
	if len(top) > 5 {
		top = top[:5]
	}

	watch := make([]model.Entry, 0)
	for _, e := range board {
		total := e.LeadsWon + e.LeadsDropped
		if total == 0 || e.LeadsDropped < 2 {
			continue
		}
		rate := float64(e.LeadsDropped) / float64(total)
		if rate >= 0.3 {
			e := e
			e.DropRate = &rate
			watch = append(watch, e)
		}
	}
	sort.SliceStable(watch, func(i, j int) bool {
		if *watch[i].DropRate != *watch[j].DropRate {
			return *watch[i].DropRate > *watch[j].DropRate
		}
		return watch[i].LeadsDropped > watch[j].LeadsDropped
	})
	if len(watch) > 5 {
		watch = watch[:5]
	}

	quarter, err := s.quarterBoard(ctx, users)
	if err != nil {
		return nil, err
	}

	out := &model.TeamOverview{
		Range: r.Key, Today: daily[len(daily)-1], Yesterday: daily[len(daily)-2], Daily: daily, Period: period,
		Top: top, Watchlist: watch, Quarter: *quarter, TotalBdas: len(users),
	}
	s.cache.SetJSON(ctx, cacheKey, out, readCacheTTL)
	return out, nil
}

func (s *Service) DailyReport(ctx context.Context, date string) (*model.DailyReport, error) {
	if date == "" {
		date = key(s.today())
	}
	if _, err := time.ParseInLocation(dateLayout, date, s.loc); err != nil {
		return nil, fmt.Errorf("%w: date must be YYYY-MM-DD", ErrBadInput)
	}
	minDate, err := s.store.MinDate(ctx)
	if err != nil {
		return nil, err
	}
	if minDate == "" || date < minDate || date > key(s.today()) {
		return &model.DailyReport{Date: date, Rows: []model.ReportRow{}, Available: false, MinDate: minDate}, nil
	}
	users, err := s.store.ListBDAs(ctx)
	if err != nil {
		return nil, err
	}
	docs, err := s.store.DailyRange(ctx, "", date, date)
	if err != nil {
		return nil, err
	}
	byBda := make(map[string]model.Daily, len(docs))
	for _, d := range docs {
		byBda[d.BdaID] = d
	}
	rows := make([]model.ReportRow, 0, len(users))
	totals := model.DaySum{Date: date}
	for _, u := range users {
		d, ok := byBda[u.ID]
		if !ok {
			d = model.Daily{BdaID: u.ID, Date: date}
		}
		rows = append(rows, model.ReportRow{Bda: ref(u), Daily: d})
		totals.Calls += d.Calls
		totals.CallMinutes += d.CallMinutes
		totals.LeadsWon += d.LeadsWon
		totals.LeadsDropped += d.LeadsDropped
		totals.Points += d.Points
		if d.Calls > 0 {
			totals.Active++
		}
		if d.LoggedIn {
			totals.LoggedIn++
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Points != b.Points {
			return a.Points > b.Points
		}
		if a.LeadsWon != b.LeadsWon {
			return a.LeadsWon > b.LeadsWon
		}
		return a.Calls > b.Calls
	})
	return &model.DailyReport{Date: date, Rows: rows, Totals: &totals, Available: true, MinDate: minDate}, nil
}

// ---------- quarterly cycles ----------

func quarterOf(t time.Time) (year, quarter int) {
	return t.Year(), (int(t.Month())-1)/3 + 1
}

func quarterStart(year, q int, loc *time.Location) time.Time {
	return time.Date(year, time.Month((q-1)*3+1), 1, 0, 0, 0, 0, loc)
}

func quarterEnd(year, q int, loc *time.Location) time.Time {
	return quarterStart(year, q, loc).AddDate(0, 3, -1)
}

// quarterBoard ranks everyone over the cycle currently in progress.
func (s *Service) quarterBoard(ctx context.Context, users []model.User) (*model.QuarterBoard, error) {
	today := s.today()
	year, q := quarterOf(today)
	start := quarterStart(year, q, s.loc)
	aggs, err := s.store.AggregateByBDA(ctx, key(start), key(today))
	if err != nil {
		return nil, err
	}
	entries := rank(users, aggs, s.currentStreak)
	top := entries
	if len(top) > 3 {
		top = top[:3]
	}
	return &model.QuarterBoard{
		Year:    year,
		Quarter: q,
		EndsOn:  key(quarterEnd(year, q, s.loc)),
		Days:    int(today.Sub(start).Hours()/24) + 1,
		Top:     top,
	}, nil
}

// NoticeBoard lists the top three of every completed three-month cycle,
// newest first, plus the cycle currently running.
func (s *Service) NoticeBoard(ctx context.Context) (*model.NoticeBoard, error) {
	var cached model.NoticeBoard
	if s.cache.GetJSON(ctx, "notice", &cached) {
		return &cached, nil
	}

	users, err := s.store.ListBDAs(ctx)
	if err != nil {
		return nil, err
	}
	today := s.today()
	byMonth, err := s.store.AggregateByBDAMonth(ctx, "", key(today))
	if err != nil {
		return nil, err
	}
	minDate, err := s.store.MinDate(ctx)
	if err != nil {
		return nil, err
	}
	if minDate == "" {
		empty := &model.NoticeBoard{Years: []model.NoticeYear{}}
		return empty, nil
	}

	// Fold months into quarters.
	type bucket struct {
		year, quarter int
		aggs          map[string]model.Agg
	}
	buckets := map[string]*bucket{}
	for month, perBda := range byMonth {
		t, err := time.ParseInLocation("2006-01", month, s.loc)
		if err != nil {
			continue
		}
		y, q := quarterOf(t)
		id := fmt.Sprintf("%d-Q%d", y, q)
		b := buckets[id]
		if b == nil {
			b = &bucket{year: y, quarter: q, aggs: map[string]model.Agg{}}
			buckets[id] = b
		}
		for id, a := range perBda {
			cur := b.aggs[id]
			cur.Calls += a.Calls
			cur.CallMinutes += a.CallMinutes
			cur.LeadsWon += a.LeadsWon
			cur.LeadsDropped += a.LeadsDropped
			cur.Points += a.Points
			cur.LoginPoints += a.LoginPoints
			cur.LeadPoints += a.LeadPoints
			cur.PenaltyPoints += a.PenaltyPoints
			cur.ActiveDays += a.ActiveDays
			cur.LoginDays += a.LoginDays
			b.aggs[id] = cur
		}
	}

	curYear, curQ := quarterOf(today)
	cycles := make([]model.Cycle, 0, len(buckets))
	for id, b := range buckets {
		start := quarterStart(b.year, b.quarter, s.loc)
		end := quarterEnd(b.year, b.quarter, s.loc)
		running := b.year == curYear && b.quarter == curQ
		// Skip a closed quarter we only have partial history for — its
		// ranking would not be a fair record.
		if !running && key(start) < minDate {
			continue
		}
		entries := rank(users, b.aggs, s.currentStreak)
		scored := make([]model.Entry, 0, len(entries))
		for _, e := range entries {
			if e.Calls > 0 || e.Points != 0 {
				scored = append(scored, e)
			}
		}
		if len(scored) == 0 {
			continue
		}
		for i := range scored {
			scored[i].Rank = i + 1
			scored[i].RankChange = nil
		}
		var totals model.Agg
		for _, e := range scored {
			totals.Calls += e.Calls
			totals.LeadsWon += e.LeadsWon
			totals.LeadsDropped += e.LeadsDropped
			totals.Points += e.Points
		}
		last := end
		if running {
			last = today
		}
		top := scored
		if len(top) > 3 {
			top = top[:3]
		}
		status := "closed"
		if running {
			status = "running"
		}
		cycles = append(cycles, model.Cycle{
			ID: id, Year: b.year, Quarter: b.quarter, From: key(start), To: key(last),
			EndsOn: key(end), Status: status, Days: int(last.Sub(start).Hours()/24) + 1,
			Participants: len(scored), Top: top, Totals: totals,
		})
	}
	sort.SliceStable(cycles, func(i, j int) bool {
		if cycles[i].Year != cycles[j].Year {
			return cycles[i].Year > cycles[j].Year
		}
		return cycles[i].Quarter > cycles[j].Quarter
	})

	out := &model.NoticeBoard{Years: []model.NoticeYear{}}
	for _, c := range cycles {
		if c.Status == "running" {
			cc := c
			out.Current = &cc
			continue
		}
		idx := -1
		for i := range out.Years {
			if out.Years[i].Year == c.Year {
				idx = i
			}
		}
		if idx == -1 {
			out.Years = append(out.Years, model.NoticeYear{Year: c.Year})
			idx = len(out.Years) - 1
		}
		out.Years[idx].Cycles = append(out.Years[idx].Cycles, c)
	}
	s.cache.SetJSON(ctx, "notice", out, 5*time.Minute)
	return out, nil
}

// ---------- roster admin ----------

// Roster lists every associate with all-time totals, for the manager screen.
func (s *Service) Roster(ctx context.Context) (*model.Roster, error) {
	users, err := s.store.ListBDAs(ctx)
	if err != nil {
		return nil, err
	}
	aggs, err := s.store.AggregateByBDA(ctx, "", key(s.today()))
	if err != nil {
		return nil, err
	}
	ranked := rank(users, aggs, s.currentStreak)
	byID := make(map[string]model.Entry, len(ranked))
	for _, e := range ranked {
		byID[e.Bda.ID] = e
	}
	entries := make([]model.RosterEntry, 0, len(users))
	for _, u := range users {
		e := byID[u.ID]
		streak, today := s.currentStreak(u)
		entries = append(entries, model.RosterEntry{
			Bda: ref(u), Joined: u.Joined, Streak: streak, BestStreak: u.BestStreak, LoggedInToday: today,
			Rank: e.Rank, Points: e.Points, Calls: e.Calls, LeadsWon: e.LeadsWon, LeadsDropped: e.LeadsDropped,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Bda.Name < entries[j].Bda.Name })
	return &model.Roster{Entries: entries}, nil
}

// CreateBDA registers a new associate. They start with no history.
func (s *Service) CreateBDA(ctx context.Context, name, email, password string) (*model.User, error) {
	name = strings.TrimSpace(name)
	email = strings.ToLower(strings.TrimSpace(email))
	if len(name) < 2 {
		return nil, fmt.Errorf("%w: enter the full name", ErrBadInput)
	}
	if !emailRe.MatchString(email) {
		return nil, fmt.Errorf("%w: enter a valid work email", ErrBadInput)
	}
	if len(password) < 6 {
		return nil, fmt.Errorf("%w: password must be at least 6 characters", ErrBadInput)
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil, err
	}
	u := model.User{
		ID:           "bda-" + strconv.FormatInt(s.now().UnixNano(), 36),
		Role:         "BDA",
		Name:         name,
		Email:        email,
		PasswordHash: hash,
		Joined:       key(s.today()),
		CreatedAt:    s.now(),
	}
	if err := s.store.InsertUser(ctx, u); err != nil {
		if errors.Is(err, store.ErrDuplicateEmail) {
			return nil, fmt.Errorf("%w: that email is already registered", ErrBadInput)
		}
		return nil, err
	}
	_ = s.cache.Invalidate(ctx)
	return &u, nil
}

// ---------- demo ----------

// ResetDemo wipes Mongo + Redis and reseeds. seedFn is injected to avoid an import cycle.
func (s *Service) ResetDemo(ctx context.Context, seedFn func(context.Context, *store.Store, time.Time) error) error {
	if err := s.store.Reset(ctx); err != nil {
		return err
	}
	if err := s.cache.FlushAll(ctx); err != nil {
		return err
	}
	if err := seedFn(ctx, s.store, s.now()); err != nil {
		return err
	}
	slog.Info("demo data reset")
	return nil
}
