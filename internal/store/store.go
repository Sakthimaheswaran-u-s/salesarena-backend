// Package store is the MongoDB persistence layer.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/salesarena/backend/internal/model"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrDuplicateEmail = errors.New("email already registered")
)

type Store struct {
	client *mongo.Client
	db     *mongo.Database
	users  *mongo.Collection
	daily  *mongo.Collection
	events *mongo.Collection
}

func New(ctx context.Context, uri, dbName string) (*Store, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(5*time.Second))
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}
	db := client.Database(dbName)
	s := &Store{
		client: client,
		db:     db,
		users:  db.Collection("users"),
		daily:  db.Collection("daily_activity"),
		events: db.Collection("events"),
	}
	if err := s.ensureIndexes(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close(ctx context.Context) error { return s.client.Disconnect(ctx) }

func (s *Store) ensureIndexes(ctx context.Context) error {
	_, err := s.users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	_, err = s.daily.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "date", Value: 1}, {Key: "bdaId", Value: 1}}},
		{Keys: bson.D{{Key: "bdaId", Value: 1}, {Key: "date", Value: 1}}},
	})
	if err != nil {
		return err
	}
	_, err = s.events.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "bdaId", Value: 1}, {Key: "at", Value: -1}}},
		{Keys: bson.D{{Key: "bdaId", Value: 1}, {Key: "date", Value: 1}}},
	})
	return err
}

// ---------- users ----------

func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	return s.users.CountDocuments(ctx, bson.M{})
}

func (s *Store) InsertUsers(ctx context.Context, users []model.User) error {
	docs := make([]any, len(users))
	for i := range users {
		docs[i] = users[i]
	}
	_, err := s.users.InsertMany(ctx, docs)
	return err
}

// InsertUser adds one account, rejecting a duplicate email.
func (s *Store) InsertUser(ctx context.Context, u model.User) error {
	_, err := s.users.InsertOne(ctx, u)
	if mongo.IsDuplicateKeyError(err) {
		return ErrDuplicateEmail
	}
	return err
}

func (s *Store) UserByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User
	err := s.users.FindOne(ctx, bson.M{"email": email}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (s *Store) UserByID(ctx context.Context, id string) (*model.User, error) {
	var u model.User
	err := s.users.FindOne(ctx, bson.M{"_id": id}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &u, err
}

// ListBDAs returns all associates sorted by name.
func (s *Store) ListBDAs(ctx context.Context) ([]model.User, error) {
	cur, err := s.users.Find(ctx, bson.M{"role": "BDA"}, options.Find().SetSort(bson.D{{Key: "name", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []model.User
	return out, cur.All(ctx, &out)
}

func (s *Store) UpdateUserStreak(ctx context.Context, id string, current, best int, lastLogin string) error {
	_, err := s.users.UpdateByID(ctx, id, bson.M{"$set": bson.M{
		"currentStreak": current, "bestStreak": best, "lastLoginDate": lastLogin,
	}})
	return err
}

// ---------- daily rollups ----------

func DailyID(bdaID, date string) string { return bdaID + ":" + date }

func (s *Store) InsertDaily(ctx context.Context, docs []model.Daily) error {
	if len(docs) == 0 {
		return nil
	}
	anyDocs := make([]any, len(docs))
	for i := range docs {
		docs[i].ID = DailyID(docs[i].BdaID, docs[i].Date)
		anyDocs[i] = docs[i]
	}
	_, err := s.daily.InsertMany(ctx, anyDocs, options.InsertMany().SetOrdered(false))
	return err
}

// UpsertDaily applies $inc and $set to one rollup, creating it if needed.
func (s *Store) UpsertDaily(ctx context.Context, bdaID, date string, inc, set bson.M) error {
	update := bson.M{"$setOnInsert": bson.M{"bdaId": bdaID, "date": date}}
	if len(inc) > 0 {
		update["$inc"] = inc
	}
	if len(set) > 0 {
		update["$set"] = set
	}
	_, err := s.daily.UpdateByID(ctx, DailyID(bdaID, date), update, options.Update().SetUpsert(true))
	return err
}

// DailyRange returns rollups for one BDA ("" = all BDAs) between from and to
// (inclusive, YYYY-MM-DD). An empty from means no lower bound.
func (s *Store) DailyRange(ctx context.Context, bdaID, from, to string) ([]model.Daily, error) {
	filter := bson.M{"date": dateFilter(from, to)}
	if bdaID != "" {
		filter["bdaId"] = bdaID
	}
	cur, err := s.daily.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "date", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var out []model.Daily
	return out, cur.All(ctx, &out)
}

func (s *Store) DailyOne(ctx context.Context, bdaID, date string) (*model.Daily, error) {
	var d model.Daily
	err := s.daily.FindOne(ctx, bson.M{"_id": DailyID(bdaID, date)}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNotFound
	}
	return &d, err
}

// MinDate returns the earliest date with any activity.
func (s *Store) MinDate(ctx context.Context) (string, error) {
	var d model.Daily
	err := s.daily.FindOne(ctx, bson.M{}, options.FindOne().SetSort(bson.D{{Key: "date", Value: 1}})).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return "", nil
	}
	return d.Date, err
}

// AggregateByBDA sums rollups per associate over the window.
func (s *Store) AggregateByBDA(ctx context.Context, from, to string) (map[string]model.Agg, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"date": dateFilter(from, to)}}},
		{{Key: "$group", Value: bson.M{
			"_id":           "$bdaId",
			"calls":         bson.M{"$sum": "$calls"},
			"callMinutes":   bson.M{"$sum": "$callMinutes"},
			"leadsWon":      bson.M{"$sum": "$leadsWon"},
			"leadsDropped":  bson.M{"$sum": "$leadsDropped"},
			"points":        bson.M{"$sum": "$points"},
			"loginPoints":   bson.M{"$sum": "$loginPoints"},
			"leadPoints":    bson.M{"$sum": "$leadPoints"},
			"penaltyPoints": bson.M{"$sum": "$penaltyPoints"},
			"activeDays":    bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$gt": bson.A{"$calls", 0}}, 1, 0}}},
			"loginDays":     bson.M{"$sum": bson.M{"$cond": bson.A{"$loggedIn", 1, 0}}},
		}}},
	}
	cur, err := s.daily.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID        string `bson:"_id"`
		model.Agg `bson:",inline"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make(map[string]model.Agg, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Agg
	}
	return out, nil
}

// AggregateByBDAMonth sums rollups per associate per calendar month. One query
// feeds every quarterly cycle on the notice board.
func (s *Store) AggregateByBDAMonth(ctx context.Context, from, to string) (map[string]map[string]model.Agg, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"date": dateFilter(from, to)}}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"bdaId": "$bdaId",
				"month": bson.M{"$substrBytes": bson.A{"$date", 0, 7}},
			},
			"calls":         bson.M{"$sum": "$calls"},
			"callMinutes":   bson.M{"$sum": "$callMinutes"},
			"leadsWon":      bson.M{"$sum": "$leadsWon"},
			"leadsDropped":  bson.M{"$sum": "$leadsDropped"},
			"points":        bson.M{"$sum": "$points"},
			"loginPoints":   bson.M{"$sum": "$loginPoints"},
			"leadPoints":    bson.M{"$sum": "$leadPoints"},
			"penaltyPoints": bson.M{"$sum": "$penaltyPoints"},
			"activeDays":    bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$gt": bson.A{"$calls", 0}}, 1, 0}}},
			"loginDays":     bson.M{"$sum": bson.M{"$cond": bson.A{"$loggedIn", 1, 0}}},
		}}},
	}
	cur, err := s.daily.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID struct {
			BdaID string `bson:"bdaId"`
			Month string `bson:"month"`
		} `bson:"_id"`
		model.Agg `bson:",inline"`
	}
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	// month -> bdaId -> agg
	out := map[string]map[string]model.Agg{}
	for _, r := range rows {
		m := out[r.ID.Month]
		if m == nil {
			m = map[string]model.Agg{}
			out[r.ID.Month] = m
		}
		m[r.ID.BdaID] = r.Agg
	}
	return out, nil
}

// AggregateByDate sums rollups across all associates per date, ascending.
func (s *Store) AggregateByDate(ctx context.Context, from, to string) ([]model.DaySum, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"date": dateFilter(from, to)}}},
		{{Key: "$group", Value: bson.M{
			"_id":          "$date",
			"calls":        bson.M{"$sum": "$calls"},
			"callMinutes":  bson.M{"$sum": "$callMinutes"},
			"leadsWon":     bson.M{"$sum": "$leadsWon"},
			"leadsDropped": bson.M{"$sum": "$leadsDropped"},
			"points":       bson.M{"$sum": "$points"},
			"active":       bson.M{"$sum": bson.M{"$cond": bson.A{bson.M{"$gt": bson.A{"$calls", 0}}, 1, 0}}},
			"loggedIn":     bson.M{"$sum": bson.M{"$cond": bson.A{"$loggedIn", 1, 0}}},
		}}},
		{{Key: "$sort", Value: bson.D{{Key: "_id", Value: 1}}}},
	}
	cur, err := s.daily.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var out []model.DaySum
	return out, cur.All(ctx, &out)
}

// ---------- events ----------

func (s *Store) InsertEvent(ctx context.Context, ev model.Event) error {
	_, err := s.events.InsertOne(ctx, ev)
	return err
}

func (s *Store) InsertEvents(ctx context.Context, evs []model.Event) error {
	if len(evs) == 0 {
		return nil
	}
	docs := make([]any, len(evs))
	for i := range evs {
		docs[i] = evs[i]
	}
	_, err := s.events.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	return err
}

// EventsByBDA returns events newest first; limit 0 = no limit.
func (s *Store) EventsByBDA(ctx context.Context, bdaID string, limit int64) ([]model.Event, error) {
	opts := options.Find().SetSort(bson.D{{Key: "at", Value: -1}})
	if limit > 0 {
		opts.SetLimit(limit)
	}
	cur, err := s.events.Find(ctx, bson.M{"bdaId": bdaID}, opts)
	if err != nil {
		return nil, err
	}
	var out []model.Event
	return out, cur.All(ctx, &out)
}

// ---------- maintenance ----------

// Reset drops every collection (used by the demo reset endpoint).
func (s *Store) Reset(ctx context.Context) error {
	for _, c := range []*mongo.Collection{s.users, s.daily, s.events} {
		if err := c.Drop(ctx); err != nil {
			return err
		}
	}
	return s.ensureIndexes(ctx)
}

func dateFilter(from, to string) bson.M {
	f := bson.M{"$lte": to}
	if from != "" {
		f["$gte"] = from
	}
	return f
}
