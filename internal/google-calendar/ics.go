// Package googlecalendar reads Google Calendar feeds published as iCalendar (the
// "secret address in iCal format" from calendar settings) and expands them into
// concrete events on a date range.
//
// The directory is google-calendar but the package name is googlecalendar: Go
// package names are identifiers, so they cannot contain a hyphen. Import sites
// therefore alias the import explicitly.
package googlecalendar

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

// property is one unfolded iCalendar content line: NAME;PARAM=V:VALUE.
type property struct {
	Name   string
	Params map[string]string
	Value  string
}

func (p property) param(key string) string { return p.Params[strings.ToUpper(key)] }

// vevent is a raw VEVENT block, before recurrence expansion.
type vevent struct {
	UID          string
	Summary      string
	Description  string
	Location     string
	Status       string
	Start        time.Time
	End          time.Time
	AllDay       bool
	HasEnd       bool
	Transparent  bool
	URL          string
	Conference   string
	Organizer    Attendee
	Attendees    []Attendee
	RRule        string
	ExDates      []time.Time
	RecurrenceID *time.Time // set on override instances
	Sequence     int
}

// parseAttendee reads an ATTENDEE or ORGANIZER line. The value is a
// CAL-ADDRESS ("mailto:someone@example.com") and the parameters carry the
// display name, the role, and the reply.
func parseAttendee(p property) Attendee {
	addr := strings.TrimSpace(p.Value)
	if len(addr) >= 7 && strings.EqualFold(addr[:7], "mailto:") {
		addr = addr[7:]
	}
	return Attendee{
		Name:     unescapeText(p.param("CN")),
		Email:    addr,
		Status:   strings.ToUpper(p.param("PARTSTAT")),
		Optional: strings.EqualFold(p.param("ROLE"), "OPT-PARTICIPANT"),
		Resource: strings.EqualFold(p.param("CUTYPE"), "RESOURCE"),
	}
}

// unfold reads iCalendar content lines, joining RFC 5545 folded continuations
// (a line beginning with a space or tab continues the previous one).
func unfold(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var lines []string
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	return lines, sc.Err()
}

// parseProperty splits "NAME;PARAM=VAL;P2=\"a:b\":VALUE" into its parts. Quoted
// parameter values may legally contain ':' and ';', so the split is stateful.
func parseProperty(line string) (property, bool) {
	var (
		name    string
		params  = map[string]string{}
		inQuote bool
		colon   = -1
	)
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case ':':
			if !inQuote {
				colon = i
			}
		}
		if colon >= 0 {
			break
		}
	}
	if colon < 0 {
		return property{}, false
	}
	head, value := line[:colon], line[colon+1:]

	segs := splitUnquoted(head, ';')
	if len(segs) == 0 || segs[0] == "" {
		return property{}, false
	}
	name = strings.ToUpper(segs[0])
	for _, seg := range segs[1:] {
		k, v, ok := strings.Cut(seg, "=")
		if !ok {
			continue
		}
		params[strings.ToUpper(k)] = strings.Trim(v, `"`)
	}
	return property{Name: name, Params: params, Value: value}, true
}

func splitUnquoted(s string, sep byte) []string {
	var out []string
	var start int
	var inQuote bool
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case sep:
			if !inQuote {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// unescapeText reverses RFC 5545 TEXT escaping.
func unescapeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n', 'N':
			b.WriteByte('\n')
		case ',', ';', '\\':
			b.WriteByte(s[i])
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// parseDateTime reads a DATE or DATE-TIME value, honouring VALUE=DATE, a
// trailing Z for UTC, and TZID. Floating times fall back to loc.
func parseDateTime(p property, loc *time.Location) (t time.Time, allDay bool, err error) {
	v := strings.TrimSpace(p.Value)
	if p.param("VALUE") == "DATE" || len(v) == 8 {
		t, err = time.ParseInLocation("20060102", v, loc)
		return t, true, err
	}
	if strings.HasSuffix(v, "Z") {
		t, err = time.ParseInLocation("20060102T150405Z", v, time.UTC)
		return t.In(loc), false, err
	}
	zone := loc
	if tzid := p.param("TZID"); tzid != "" {
		if z, zerr := time.LoadLocation(tzid); zerr == nil {
			zone = z
		}
	}
	t, err = time.ParseInLocation("20060102T150405", v, zone)
	return t, false, err
}

// parseCalendar reads an ICS stream into raw VEVENTs. Malformed individual
// properties are skipped rather than failing the whole feed, since a single bad
// event should not blank the calendar pane.
func parseCalendar(r io.Reader, loc *time.Location) ([]vevent, error) {
	lines, err := unfold(r)
	if err != nil {
		return nil, err
	}
	// A calendar-level TZID (X-WR-TIMEZONE) is Google's hint for floating times.
	calendarLoc := loc
	for _, line := range lines {
		if p, ok := parseProperty(line); ok && p.Name == "X-WR-TIMEZONE" {
			if z, zerr := time.LoadLocation(strings.TrimSpace(p.Value)); zerr == nil {
				calendarLoc = z
			}
			break
		}
	}

	var (
		events  []vevent
		cur     *vevent
		inEvent bool
	)
	for _, line := range lines {
		p, ok := parseProperty(line)
		if !ok {
			continue
		}
		switch p.Name {
		case "BEGIN":
			if strings.EqualFold(p.Value, "VEVENT") {
				inEvent = true
				cur = &vevent{}
			}
			continue
		case "END":
			if strings.EqualFold(p.Value, "VEVENT") && cur != nil {
				if !cur.Start.IsZero() {
					events = append(events, *cur)
				}
				cur, inEvent = nil, false
			}
			continue
		}
		if !inEvent || cur == nil {
			continue
		}

		switch p.Name {
		case "UID":
			cur.UID = p.Value
		case "SUMMARY":
			cur.Summary = unescapeText(p.Value)
		case "DESCRIPTION":
			cur.Description = unescapeText(p.Value)
		case "LOCATION":
			cur.Location = unescapeText(p.Value)
		case "STATUS":
			cur.Status = strings.ToUpper(p.Value)
		case "TRANSP":
			cur.Transparent = strings.EqualFold(p.Value, "TRANSPARENT")
		case "URL":
			cur.URL = strings.TrimSpace(p.Value)
		case "X-GOOGLE-CONFERENCE":
			cur.Conference = strings.TrimSpace(p.Value)
		case "ORGANIZER":
			cur.Organizer = parseAttendee(p)
		case "ATTENDEE":
			cur.Attendees = append(cur.Attendees, parseAttendee(p))
		case "RRULE":
			cur.RRule = p.Value
		case "SEQUENCE":
			fmt.Sscanf(p.Value, "%d", &cur.Sequence)
		case "DTSTART":
			if t, allDay, err := parseDateTime(p, calendarLoc); err == nil {
				cur.Start, cur.AllDay = t, allDay
			}
		case "DTEND":
			if t, _, err := parseDateTime(p, calendarLoc); err == nil {
				cur.End, cur.HasEnd = t, true
			}
		case "RECURRENCE-ID":
			if t, _, err := parseDateTime(p, calendarLoc); err == nil {
				cur.RecurrenceID = &t
			}
		case "EXDATE":
			for _, v := range strings.Split(p.Value, ",") {
				ep := property{Name: "EXDATE", Params: p.Params, Value: v}
				if t, _, err := parseDateTime(ep, calendarLoc); err == nil {
					cur.ExDates = append(cur.ExDates, t)
				}
			}
		}
	}
	return events, nil
}
