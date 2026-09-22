package rules

import "testing"

func TestLoginPoints(t *testing.T) {
	cases := map[int]int{0: 10, 1: 10, 2: 15, 5: 30, 11: 60, 12: 60, 40: 60}
	for day, want := range cases {
		if got := LoginPoints(day); got != want {
			t.Errorf("LoginPoints(%d) = %d, want %d", day, got, want)
		}
	}
}

func TestParseRange(t *testing.T) {
	if ParseRange("week").Days != 7 {
		t.Error("week should be 7 days")
	}
	if ParseRange("bogus").Key != "month" {
		t.Error("unknown range should default to month")
	}
	if ParseRange("all").Days != 0 {
		t.Error("all should have Days == 0")
	}
}
