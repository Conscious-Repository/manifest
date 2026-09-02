package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manifest/daily"
	"manifest/goals"
	"manifest/vault"
)

// `GET /api/day` with no date answered `400 bad date ""` — a plain-text body the
// cockpit's load() handed to r.json(), which threw before renderDay() ever ran
// and left the Day page as four static headings over four empty panels. A read
// of "the day" with no date means today; only a supplied-but-junk date is a 400.
func TestDayGETWithoutDateIsToday(t *testing.T) {
	dir := t.TempDir()
	idx, err := vault.NewIndex(vault.Config{Root: dir, GoalsName: "goals.md"})
	if err != nil {
		t.Fatal(err)
	}
	svc := daily.NewService(daily.Config{VaultPath: dir, ScheduleStart: 8, ScheduleEnd: 18, Write: testWrite}, idx)
	s := New(svc, goals.NewStore(idx, dir, "goals.md", testWrite), nil)

	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.handleDay(rec, httptest.NewRequest(http.MethodGet, url, nil))
		return rec
	}

	for _, url := range []string{"/api/day", "/api/day?date="} {
		rec := get(url)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", url, rec.Code, rec.Body.String())
		}
		var day daily.Day
		if err := json.Unmarshal(rec.Body.Bytes(), &day); err != nil {
			t.Fatalf("GET %s: body is not JSON (%v): %s", url, err, rec.Body.String())
		}
		if want := time.Now().Format("2006-01-02"); day.Date != want {
			t.Fatalf("GET %s: date %q, want today %q", url, day.Date, want)
		}
	}

	if rec := get("/api/day?date=yesterday"); rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/day?date=yesterday: %d, want 400 — a junk date is still junk", rec.Code)
	}
}
