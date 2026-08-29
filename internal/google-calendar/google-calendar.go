package googlecalendar

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teambition/rrule-go"
)

// Source is one calendar feed.
type Source struct {
	Name  string
	URL   string
	Color string
	// Email is the address whose reply counts as "mine" among an event's
	// attendees. Empty means: take it from the feed URL, which is where a
	// Google private iCal address already carries it.
	Email string
}

// Attendee is one invitee of an event. Status is the iCalendar PARTSTAT,
// uppercased: ACCEPTED, DECLINED, TENTATIVE or NEEDS-ACTION. A feed that
// carries no reply leaves it empty.
type Attendee struct {
	Name     string
	Email    string
	Status   string
	Optional bool // ROLE=OPT-PARTICIPANT
	Resource bool // CUTYPE=RESOURCE, a room rather than a person
	Self     bool // this attendee is the owner of the feed
}

// Label is what to call an attendee: the display name when the feed has one,
// the address otherwise.
func (a Attendee) Label() string {
	if strings.TrimSpace(a.Name) != "" {
		return a.Name
	}
	return a.Email
}

// Event is a concrete occurrence on the timeline. For all-day events Start is
// local midnight of the first day and End is local midnight of the day *after*
// the last day, matching iCalendar's exclusive DTEND.
type Event struct {
	UID         string
	Summary     string
	Description string
	Location    string
	Calendar    string
	Color       string
	Start       time.Time
	End         time.Time
	AllDay      bool
	// Status is the event's own STATUS (CONFIRMED or TENTATIVE; cancelled
	// events are dropped during expansion).
	Status string
	// Transparent events do not block the calendar — Google's "free".
	Transparent bool
	// Repeat describes the RRULE this occurrence came from, e.g. "every week
	// on Tue". It is empty for one-off events.
	Repeat     string
	URL        string
	Conference string // Google Meet link, from X-GOOGLE-CONFERENCE
	Organizer  Attendee
	Attendees  []Attendee
	// MyStatus is the owner's own PARTSTAT, empty when the feed does not say
	// who is who — a calendar with no guests never does.
	MyStatus string
}

// Guests counts the replies, leaving out meeting rooms and other resources.
func (e Event) Guests() (going, maybe, declined, noReply int) {
	for _, a := range e.Attendees {
		if a.Resource {
			continue
		}
		switch a.Status {
		case "ACCEPTED":
			going++
		case "TENTATIVE":
			maybe++
		case "DECLINED":
			declined++
		default:
			noReply++
		}
	}
	return going, maybe, declined, noReply
}

// OccursOn reports whether the event covers any part of the given local day.
func (e Event) OccursOn(day time.Time) bool {
	dayStart := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	dayEnd := dayStart.AddDate(0, 0, 1)
	if !e.Start.Before(dayEnd) {
		return false
	}
	if e.End.After(e.Start) {
		return e.End.After(dayStart)
	}
	// Zero-length event: it occurs iff its instant falls inside the day.
	return !e.Start.Before(dayStart)
}

// TimeLabel renders the time column of the agenda list.
func (e Event) TimeLabel() string {
	if e.AllDay {
		return "all-day"
	}
	if e.End.After(e.Start) {
		return e.Start.Format("15:04") + "-" + e.End.Format("15:04")
	}
	return e.Start.Format("15:04")
}

// Client fetches ICS feeds, caching raw bodies briefly so that redrawing or
// flipping months does not re-download the whole calendar.
type Client struct {
	http *http.Client
	ttl  time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	body    []byte
	fetched time.Time
}

// NewClient returns a client caching feeds for ttl (0 uses 5 minutes).
func NewClient(ttl time.Duration) *Client {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Client{
		http:  &http.Client{Timeout: 30 * time.Second},
		ttl:   ttl,
		cache: map[string]cacheEntry{},
	}
}

// Invalidate drops all cached feeds so the next fetch hits the network.
func (c *Client) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = map[string]cacheEntry{}
}

func (c *Client) fetch(ctx context.Context, feedURL string) ([]byte, error) {
	c.mu.Lock()
	if e, ok := c.cache[feedURL]; ok && time.Since(e.fetched) < c.ttl {
		c.mu.Unlock()
		return e.body, nil
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, scrubError(feedURL, err)
	}
	req.Header.Set("User-Agent", "toolbox-tui")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, scrubError(feedURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", redactURL(feedURL), resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, scrubError(feedURL, err)
	}

	c.mu.Lock()
	c.cache[feedURL] = cacheEntry{body: body, fetched: time.Now()}
	c.mu.Unlock()
	return body, nil
}

// redactURL reduces a feed address to scheme and host. A Google private ICS
// URL is a bearer credential — anyone holding it can read the calendar — and
// its path also carries the owner's address, so the whole path goes. Errors
// already name the configured source, which is what a reader actually needs.
func redactURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil || parsed.Host == "" {
		return "<redacted url>"
	}
	return parsed.Scheme + "://" + parsed.Host + "/…"
}

// secretSegment matches the token in a Google private ICS path, as a backstop
// for messages that embed the URL in a form redactURL cannot recognise.
var secretSegment = regexp.MustCompile(`private-[A-Za-z0-9_\-]+`)

// scrubError removes a feed URL from an error message. net/http puts the full
// URL, secret and all, into every transport error, and those errors are shown
// in the footer and printed by `tt doctor` — so they must never carry it.
func scrubError(feedURL string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	msg = strings.ReplaceAll(msg, feedURL, redactURL(feedURL))
	if u, parseErr := url.Parse(feedURL); parseErr == nil && u.Path != "" && u.Path != "/" {
		msg = strings.ReplaceAll(msg, u.Path, "/…")
	}
	msg = secretSegment.ReplaceAllString(msg, "private-…redacted…")
	return errors.New(msg)
}

// Events expands every source over [from, to) in the given location. Per-source
// failures are returned alongside whatever did load, so one broken feed does
// not blank the pane.
func (c *Client) Events(ctx context.Context, sources []Source, from, to time.Time, loc *time.Location) ([]Event, []error) {
	type result struct {
		events []Event
		err    error
	}
	results := make([]result, len(sources))
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, src Source) {
			defer wg.Done()
			body, err := c.fetch(ctx, src.URL)
			if err != nil {
				results[i].err = fmt.Errorf("%s: %w", src.Name, err)
				return
			}
			raw, err := parseCalendar(bytes.NewReader(body), loc)
			if err != nil {
				results[i].err = fmt.Errorf("%s: %w", src.Name, scrubError(src.URL, err))
				return
			}
			results[i].events = expand(raw, src, from, to)
		}(i, src)
	}
	wg.Wait()

	var all []Event
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		all = append(all, r.events...)
	}
	sort.SliceStable(all, func(a, b int) bool {
		if all[a].AllDay != all[b].AllDay {
			return all[a].AllDay // all-day events sit above timed ones
		}
		if !all[a].Start.Equal(all[b].Start) {
			return all[a].Start.Before(all[b].Start)
		}
		return all[a].Summary < all[b].Summary
	})
	return all, errs
}

// expand turns raw VEVENTs into concrete occurrences inside [from, to),
// applying RRULE, EXDATE, and RECURRENCE-ID overrides.
func expand(raw []vevent, src Source, from, to time.Time) []Event {
	self := strings.ToLower(firstNonEmpty(src.Email, ownerFromURL(src.URL)))

	// Group by UID: one base event plus any modified-instance overrides.
	bases := map[string]vevent{}
	overrides := map[string][]vevent{}
	var order []string
	for _, v := range raw {
		if v.RecurrenceID != nil {
			overrides[v.UID] = append(overrides[v.UID], v)
			continue
		}
		if _, seen := bases[v.UID]; !seen {
			order = append(order, v.UID)
		}
		// A later SEQUENCE supersedes an earlier one for the same UID.
		if prev, ok := bases[v.UID]; !ok || v.Sequence >= prev.Sequence {
			bases[v.UID] = v
		}
	}

	var out []Event
	emit := func(v vevent, start time.Time) {
		if strings.EqualFold(v.Status, "CANCELLED") {
			return
		}
		end := start.Add(duration(v))
		if !start.Before(to) || !end.After(from) {
			// Zero-length events still count when they land inside the window.
			if !(end.Equal(start) && !start.Before(from) && start.Before(to)) {
				return
			}
		}
		// The feed does not mark an attendee as "me", so the owner's address
		// decides whose reply is the event's own.
		attendees := append([]Attendee(nil), v.Attendees...)
		var mine string
		for i := range attendees {
			if self != "" && strings.EqualFold(attendees[i].Email, self) {
				attendees[i].Self = true
				mine = attendees[i].Status
			}
		}
		out = append(out, Event{
			UID:         v.UID,
			Summary:     firstNonEmpty(v.Summary, "(no title)"),
			Description: v.Description,
			Location:    v.Location,
			Calendar:    src.Name,
			Color:       src.Color,
			Start:       start,
			End:         end,
			AllDay:      v.AllDay,
			Status:      v.Status,
			Transparent: v.Transparent,
			Repeat:      repeatLabel(v.RRule),
			URL:         v.URL,
			Conference:  v.Conference,
			Organizer:   v.Organizer,
			Attendees:   attendees,
			MyStatus:    mine,
		})
	}

	for _, uid := range order {
		base := bases[uid]
		ovs := overrides[uid]

		if base.RRule == "" {
			emit(base, base.Start)
		} else {
			set, err := buildSet(base, ovs)
			if err != nil {
				// An unparsable rule still deserves its first occurrence.
				emit(base, base.Start)
			} else {
				// Pad the lower bound by the event length so a long occurrence
				// that started before the window is still found.
				lower := from.Add(-duration(base)).Add(-time.Second)
				for _, t := range set.Between(lower, to, true) {
					emit(base, t)
				}
			}
		}
		for _, ov := range ovs {
			emit(ov, ov.Start)
		}
	}

	// Overrides for UIDs whose base event is absent from the feed window.
	for uid, ovs := range overrides {
		if _, ok := bases[uid]; ok {
			continue
		}
		for _, ov := range ovs {
			emit(ov, ov.Start)
		}
	}
	return out
}

func buildSet(base vevent, overrides []vevent) (*rrule.Set, error) {
	opts, err := rrule.StrToROptionInLocation(base.RRule, base.Start.Location())
	if err != nil {
		return nil, err
	}
	opts.Dtstart = base.Start
	r, err := rrule.NewRRule(*opts)
	if err != nil {
		return nil, err
	}
	set := &rrule.Set{}
	set.DTStart(base.Start)
	set.RRule(r)
	for _, ex := range base.ExDates {
		set.ExDate(ex)
	}
	// An override replaces its original instance, so exclude the original slot.
	for _, ov := range overrides {
		if ov.RecurrenceID != nil {
			set.ExDate(*ov.RecurrenceID)
		}
	}
	return set, nil
}

// ownerFromURL recovers the calendar's own address from a Google iCal feed
// URL, which has the shape
// .../calendar/ical/<calendar id>/private-<token>/basic.ics and whose calendar
// id is the owner's address. Only the address is taken; the secret never
// leaves the fetch path.
func ownerFromURL(feedURL string) string {
	u, err := url.Parse(feedURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if strings.EqualFold(p, "ical") && i+1 < len(parts) && strings.Contains(parts[i+1], "@") {
			return parts[i+1]
		}
	}
	return ""
}

// repeatLabel turns an RRULE into something a person can read. It covers the
// shapes Google emits; anything else falls back to the rule itself, which is
// still more use than nothing.
func repeatLabel(rule string) string {
	if strings.TrimSpace(rule) == "" {
		return ""
	}
	parts := map[string]string{}
	for _, seg := range strings.Split(rule, ";") {
		if k, v, ok := strings.Cut(seg, "="); ok {
			parts[strings.ToUpper(k)] = strings.ToUpper(strings.TrimSpace(v))
		}
	}
	unit := map[string]string{
		"DAILY": "day", "WEEKLY": "week", "MONTHLY": "month", "YEARLY": "year",
	}[parts["FREQ"]]
	if unit == "" {
		return strings.ToLower(rule)
	}
	label := "every " + unit
	if n := parts["INTERVAL"]; n != "" && n != "1" {
		label = "every " + n + " " + unit + "s"
	}
	if days := weekdayList(parts["BYDAY"]); days != "" && parts["FREQ"] == "WEEKLY" {
		label += " on " + days
	}
	switch {
	case parts["COUNT"] != "":
		label += ", " + parts["COUNT"] + " times"
	case len(parts["UNTIL"]) >= 8:
		if t, err := time.Parse("20060102", parts["UNTIL"][:8]); err == nil {
			label += ", until " + t.Format("2006-01-02")
		}
	}
	return label
}

// weekdayList renders an RRULE BYDAY list ("MO,WE" or "2TU") as weekday names.
func weekdayList(byday string) string {
	names := map[string]string{
		"SU": "Sun", "MO": "Mon", "TU": "Tue", "WE": "Wed",
		"TH": "Thu", "FR": "Fri", "SA": "Sat",
	}
	var out []string
	for _, d := range strings.Split(byday, ",") {
		d = strings.TrimLeft(strings.TrimSpace(d), "+-0123456789")
		if n, ok := names[d]; ok {
			out = append(out, n)
		}
	}
	return strings.Join(out, " ")
}

// duration returns the event length, defaulting to one day for all-day events
// and to zero for timed events, per RFC 5545.
func duration(v vevent) time.Duration {
	if v.HasEnd && v.End.After(v.Start) {
		return v.End.Sub(v.Start)
	}
	if v.AllDay {
		return 24 * time.Hour
	}
	return 0
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
