package googlecalendar

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

var jst = time.FixedZone("JST", 9*3600)

// ics joins lines with CRLF the way a real feed does.
func ics(lines ...string) string { return strings.Join(lines, "\r\n") + "\r\n" }

func TestUnfoldJoinsContinuationLines(t *testing.T) {
	got, err := unfold(strings.NewReader("SUMMARY:Hello\r\n  World\r\nUID:1\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"SUMMARY:Hello World", "UID:1"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unfold = %q, want %q", got, want)
	}
}

func TestParsePropertyHandlesQuotedParams(t *testing.T) {
	p, ok := parseProperty(`DTSTART;TZID="Asia/Tokyo:odd";VALUE=DATE-TIME:20260828T100000`)
	if !ok {
		t.Fatal("parse failed")
	}
	if p.Name != "DTSTART" {
		t.Errorf("name = %q", p.Name)
	}
	if got := p.param("TZID"); got != "Asia/Tokyo:odd" {
		t.Errorf("TZID = %q", got)
	}
	if p.Value != "20260828T100000" {
		t.Errorf("value = %q", p.Value)
	}
}

func TestUnescapeText(t *testing.T) {
	if got := unescapeText(`a\nb\, c\; d\\e`); got != "a\nb, c; d\\e" {
		t.Errorf("got %q", got)
	}
}

func TestParseAllDayAndTimedEvents(t *testing.T) {
	feed := ics(
		"BEGIN:VCALENDAR",
		"X-WR-TIMEZONE:Asia/Tokyo",
		"BEGIN:VEVENT",
		"UID:allday@example.com",
		"SUMMARY:Holiday",
		"DTSTART;VALUE=DATE:20260828",
		"DTEND;VALUE=DATE:20260830",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:timed@example.com",
		"SUMMARY:Standup",
		"DTSTART;TZID=Asia/Tokyo:20260828T100000",
		"DTEND;TZID=Asia/Tokyo:20260828T103000",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:utc@example.com",
		"SUMMARY:UTC call",
		"DTSTART:20260828T010000Z",
		"DTEND:20260828T020000Z",
		"END:VEVENT",
		"END:VCALENDAR",
	)
	evs, err := parseCalendar(strings.NewReader(feed), jst)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3", len(evs))
	}
	if !evs[0].AllDay {
		t.Error("first event should be all-day")
	}
	if got := evs[0].Start.Format("2006-01-02"); got != "2026-08-28" {
		t.Errorf("all-day start = %s", got)
	}
	if got := evs[1].Start.Format("15:04"); got != "10:00" {
		t.Errorf("timed start = %s", got)
	}
	// 01:00Z is 10:00 JST.
	if got := evs[2].Start.In(jst).Format("15:04"); got != "10:00" {
		t.Errorf("utc start in JST = %s", got)
	}
}

func TestExpandWeeklyRecurrenceWithExdateAndOverride(t *testing.T) {
	feed := ics(
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"UID:weekly@example.com",
		"SUMMARY:Weekly sync",
		"DTSTART;TZID=Asia/Tokyo:20260803T100000",
		"DTEND;TZID=Asia/Tokyo:20260803T110000",
		"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=6",
		"EXDATE;TZID=Asia/Tokyo:20260817T100000",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:weekly@example.com",
		"SUMMARY:Weekly sync (moved)",
		"RECURRENCE-ID;TZID=Asia/Tokyo:20260824T100000",
		"DTSTART;TZID=Asia/Tokyo:20260825T140000",
		"DTEND;TZID=Asia/Tokyo:20260825T150000",
		"END:VEVENT",
		"END:VCALENDAR",
	)
	raw, err := parseCalendar(strings.NewReader(feed), jst)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, jst)
	to := time.Date(2026, 9, 15, 0, 0, 0, 0, jst)
	evs := expand(raw, Source{Name: "test"}, from, to)

	var dates []string
	for _, e := range evs {
		dates = append(dates, e.Start.In(jst).Format("2006-01-02 15:04"))
	}
	// Mondays Aug 3,10,17,24,31 + Sep 7; Aug 17 excluded; Aug 24 replaced by Aug 25 14:00.
	want := map[string]bool{
		"2026-08-03 10:00": true,
		"2026-08-10 10:00": true,
		"2026-08-31 10:00": true,
		"2026-09-07 10:00": true,
		"2026-08-25 14:00": true,
	}
	if len(evs) != len(want) {
		t.Fatalf("got %d occurrences %v, want %d", len(evs), dates, len(want))
	}
	for _, d := range dates {
		if !want[d] {
			t.Errorf("unexpected occurrence %s (all: %v)", d, dates)
		}
	}
}

func TestExpandDropsCancelledAndClipsToWindow(t *testing.T) {
	feed := ics(
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"UID:cancelled@example.com",
		"SUMMARY:Gone",
		"STATUS:CANCELLED",
		"DTSTART;TZID=Asia/Tokyo:20260828T100000",
		"DTEND;TZID=Asia/Tokyo:20260828T110000",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:outside@example.com",
		"SUMMARY:Far future",
		"DTSTART;TZID=Asia/Tokyo:20270828T100000",
		"DTEND;TZID=Asia/Tokyo:20270828T110000",
		"END:VEVENT",
		"BEGIN:VEVENT",
		"UID:spanning@example.com",
		"SUMMARY:Long trip",
		"DTSTART;VALUE=DATE:20260825",
		"DTEND;VALUE=DATE:20260905",
		"END:VEVENT",
		"END:VCALENDAR",
	)
	raw, err := parseCalendar(strings.NewReader(feed), jst)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 28, 0, 0, 0, 0, jst)
	to := time.Date(2026, 8, 29, 0, 0, 0, 0, jst)
	evs := expand(raw, Source{Name: "test"}, from, to)
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1 (the spanning one)", len(evs))
	}
	if evs[0].Summary != "Long trip" {
		t.Errorf("kept %q", evs[0].Summary)
	}
}

func TestOccursOn(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 8, d, 0, 0, 0, 0, jst) }
	allDay := Event{AllDay: true, Start: day(25), End: day(28)} // exclusive end
	for _, d := range []int{25, 26, 27} {
		if !allDay.OccursOn(day(d)) {
			t.Errorf("all-day should cover Aug %d", d)
		}
	}
	if allDay.OccursOn(day(28)) {
		t.Error("all-day end is exclusive; Aug 28 should not be covered")
	}
	if allDay.OccursOn(day(24)) {
		t.Error("Aug 24 is before the event")
	}

	timed := Event{Start: day(28).Add(10 * time.Hour), End: day(28).Add(11 * time.Hour)}
	if !timed.OccursOn(day(28)) {
		t.Error("timed event should cover its own day")
	}
	if timed.OccursOn(day(27)) || timed.OccursOn(day(29)) {
		t.Error("timed event leaked into neighbouring days")
	}

	instant := Event{Start: day(28).Add(9 * time.Hour), End: day(28).Add(9 * time.Hour)}
	if !instant.OccursOn(day(28)) {
		t.Error("zero-length event should still show on its day")
	}
}

const (
	testSecret = "private-0123456789abcdef0123456789abcdef"
	testFeed   = "https://calendar.google.com/calendar/ical/abc%40gmail.com/" + testSecret + "/basic.ics"
)

func TestRedactURLKeepsOnlySchemeAndHost(t *testing.T) {
	got := redactURL(testFeed)
	if got != "https://calendar.google.com/…" {
		t.Errorf("redactURL = %q", got)
	}
	// The path carries both the credential and the owner's address.
	for _, leak := range []string{testSecret, "abc%40gmail.com", "gmail.com/calendar"} {
		if strings.Contains(got, leak) {
			t.Errorf("redactURL leaked %q: %s", leak, got)
		}
	}
	if got := redactURL("not a url at all"); strings.Contains(got, "not a url") {
		t.Errorf("a malformed URL should be dropped wholesale, got %q", got)
	}
}

// TestScrubErrorRemovesTheSecret guards the leak that mattered: net/http puts
// the full URL into every transport error, and those errors reach the footer
// and `tt doctor`.
func TestScrubErrorRemovesTheSecret(t *testing.T) {
	cases := []error{
		fmt.Errorf(`Get %q: dial tcp: lookup calendar.google.com: no such host`, testFeed),
		fmt.Errorf("read %s: connection reset", testFeed),
		// A message that embeds the secret without the whole URL around it.
		errors.New("odd wrapper mentioning " + testSecret + " on its own"),
		&url.Error{Op: "Get", URL: testFeed, Err: errors.New("timeout")},
	}
	for _, in := range cases {
		out := scrubError(testFeed, in)
		if strings.Contains(out.Error(), testSecret) {
			t.Errorf("secret survived scrubbing:\n  in:  %v\n  out: %v", in, out)
		}
		if out.Error() == "" {
			t.Errorf("scrubbing emptied the message for %v", in)
		}
	}
	if scrubError(testFeed, nil) != nil {
		t.Error("scrubError(nil) should stay nil")
	}
	// The underlying cause has to survive, or errors become useless.
	out := scrubError(testFeed, fmt.Errorf(`Get %q: no such host`, testFeed))
	if !strings.Contains(out.Error(), "no such host") {
		t.Errorf("scrubbing lost the cause: %v", out)
	}
}

func TestAttendeesCarryTheirReplies(t *testing.T) {
	feed := ics(
		"BEGIN:VCALENDAR",
		"X-WR-TIMEZONE:Asia/Tokyo",
		"BEGIN:VEVENT",
		"UID:one-on-one@example.com",
		"SUMMARY:1:1",
		"DTSTART;TZID=Asia/Tokyo:20260828T140000",
		"DTEND;TZID=Asia/Tokyo:20260828T150000",
		"ORGANIZER;CN=Alice:mailto:alice@example.com",
		"ATTENDEE;CN=Alice;PARTSTAT=ACCEPTED:mailto:alice@example.com",
		"ATTENDEE;CN=Me;PARTSTAT=TENTATIVE:MAILTO:Me@Example.com",
		"ATTENDEE;CN=Bob;ROLE=OPT-PARTICIPANT;PARTSTAT=NEEDS-ACTION:mailto:bob@example.com",
		"ATTENDEE;CUTYPE=RESOURCE;CN=Room B;PARTSTAT=ACCEPTED:mailto:room@resource.calendar.google.com",
		"TRANSP:TRANSPARENT",
		"X-GOOGLE-CONFERENCE:https://meet.example.invalid/abc-defg-hij",
		"RRULE:FREQ=WEEKLY;BYDAY=FR",
		"END:VEVENT",
		"END:VCALENDAR",
	)
	raw, err := parseCalendar(strings.NewReader(feed), jst)
	if err != nil {
		t.Fatal(err)
	}
	// The owner's address comes from the feed URL, exactly as a Google private
	// iCal address carries it.
	src := Source{
		Name: "personal",
		URL:  "https://calendar.google.com/calendar/ical/me%40example.com/private-secret/basic.ics",
	}
	from := time.Date(2026, 8, 28, 0, 0, 0, 0, jst)
	evs := expand(raw, src, from, from.AddDate(0, 0, 1))
	if len(evs) != 1 {
		t.Fatalf("got %d occurrences, want 1", len(evs))
	}
	ev := evs[0]

	if ev.MyStatus != "TENTATIVE" {
		t.Errorf("MyStatus = %q, want TENTATIVE", ev.MyStatus)
	}
	var self string
	for _, a := range ev.Attendees {
		if a.Self {
			self = a.Email
		}
	}
	if self != "Me@Example.com" {
		t.Errorf("the owner was matched to %q, want the address from the feed URL", self)
	}
	going, maybe, declined, noReply := ev.Guests()
	if going != 1 || maybe != 1 || declined != 0 || noReply != 1 {
		t.Errorf("Guests() = %d/%d/%d/%d, want 1 going, 1 maybe, 0 declined, 1 no reply (the room does not count)",
			going, maybe, declined, noReply)
	}
	if !ev.Transparent {
		t.Error("TRANSP:TRANSPARENT did not survive expansion")
	}
	if ev.Conference != "https://meet.example.invalid/abc-defg-hij" {
		t.Errorf("Conference = %q", ev.Conference)
	}
	if ev.Organizer.Label() != "Alice" {
		t.Errorf("Organizer = %q", ev.Organizer.Label())
	}
	if ev.Repeat != "every week on Fri" {
		t.Errorf("Repeat = %q", ev.Repeat)
	}
}

// TestAFeedWithoutAttendeesHasNoReply keeps a calendar with no guests from
// claiming the owner never answered.
func TestAFeedWithoutAttendeesHasNoReply(t *testing.T) {
	feed := ics(
		"BEGIN:VCALENDAR",
		"BEGIN:VEVENT",
		"UID:solo@example.com",
		"SUMMARY:Focus time",
		"DTSTART;TZID=Asia/Tokyo:20260828T090000",
		"DTEND;TZID=Asia/Tokyo:20260828T100000",
		"END:VEVENT",
		"END:VCALENDAR",
	)
	raw, err := parseCalendar(strings.NewReader(feed), jst)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 8, 28, 0, 0, 0, 0, jst)
	evs := expand(raw, Source{Name: "personal", URL: "https://example.invalid/basic.ics"}, from, from.AddDate(0, 0, 1))
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].MyStatus != "" {
		t.Errorf("MyStatus = %q, want empty", evs[0].MyStatus)
	}
}

func TestOwnerFromURL(t *testing.T) {
	cases := map[string]string{
		"https://calendar.google.com/calendar/ical/me%40example.com/private-abc/basic.ics":            "me@example.com",
		"https://calendar.google.com/calendar/ical/team%40group.calendar.google.com/public/basic.ics": "team@group.calendar.google.com",
		"https://example.invalid/feed.ics": "",
		"://nonsense":                      "",
	}
	for in, want := range cases {
		if got := ownerFromURL(in); got != want {
			t.Errorf("ownerFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepeatLabel(t *testing.T) {
	cases := map[string]string{
		"":                                   "",
		"FREQ=DAILY":                         "every day",
		"FREQ=WEEKLY;BYDAY=MO,WE":            "every week on Mon Wed",
		"FREQ=WEEKLY;INTERVAL=2;BYDAY=FR":    "every 2 weeks on Fri",
		"FREQ=MONTHLY;BYDAY=2TU;COUNT=6":     "every month, 6 times",
		"FREQ=YEARLY;UNTIL=20301231T000000Z": "every year, until 2030-12-31",
		"FREQ=HOURLY":                        "freq=hourly",
	}
	for in, want := range cases {
		if got := repeatLabel(in); got != want {
			t.Errorf("repeatLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
