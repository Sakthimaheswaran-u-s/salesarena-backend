package seed

import (
	"testing"
	"time"
)

// Known-answer test against the JS generator (values printed by node from
// frontend/src/services/mockApi.js: hashSeed('bda-1') and the first draws).
func TestPRNGMatchesJS(t *testing.T) {
	if got := hashSeed("bda-1"); got != 4253527632 {
		t.Fatalf("hashSeed(bda-1) = %d, want 4253527632", got)
	}
	if got := hashSeed("bda-12"); got != 2123859014 {
		t.Fatalf("hashSeed(bda-12) = %d, want 2123859014", got)
	}
	r := &prng{a: hashSeed("bda-1")}
	want := []float64{0.5158023044932634, 0.9293837503064424, 0.3501205963548273}
	for i, w := range want {
		if got := r.next(); got != w {
			t.Fatalf("draw %d = %v, want %v", i, got, w)
		}
	}
}

func TestGenerateRawIsDeterministicAndPlausible(t *testing.T) {
	loc := time.UTC
	dates := []string{"2026-09-14", "2026-09-15", "2026-09-16", "2026-09-19", "2026-09-20", "2026-09-21"}
	u := DemoUsers[1]
	a := generateRaw(u, dates, loc)
	b := generateRaw(u, dates, loc)
	for _, d := range dates {
		if a[d].calls != b[d].calls || len(a[d].won) != len(b[d].won) {
			t.Fatalf("non-deterministic output for %s", d)
		}
		if a[d].calls < 0 || len(a[d].won) > 5 || len(a[d].dropped) > 2 {
			t.Fatalf("implausible values for %s: %+v", d, a[d])
		}
	}
	if a["2026-09-21"].loggedIn {
		t.Fatal("today must not be pre-logged-in")
	}
	if a["2026-09-21"].calls == 0 {
		t.Fatal("today should have in-progress calls")
	}
}
