package ui

import "time"

// Hit-testing works off a map the renderer fills in as it draws. The alternative
// — recomputing the layout arithmetic inside the click handler — would be a
// second copy of the column widths, scroll offsets and row heights, free to
// drift out of step with the drawing code. Recording regions while drawing means
// a click can only ever resolve to something that was genuinely on screen.
type hitKind int

const (
	hitNone hitKind = iota
	hitBoardCard
	hitRoadmapRow
	hitCalendarDay
	hitAgendaEntry
)

// hitRegion is one clickable rectangle in screen coordinates.
type hitRegion struct {
	x, y, w, h int
	kind       hitKind

	col, row int       // board
	index    int       // roadmap row, or agenda entry
	day      time.Time // calendar cell
}

func (r hitRegion) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// hitmap is held by pointer on the model so View, which takes its receiver by
// value, can still record into it.
type hitmap struct {
	regions []hitRegion
}

func (h *hitmap) reset() {
	if h == nil {
		return
	}
	h.regions = h.regions[:0]
}

func (h *hitmap) add(r hitRegion) {
	if h == nil || r.w <= 0 || r.h <= 0 {
		return
	}
	h.regions = append(h.regions, r)
}

// at returns the region under the point. Later regions win, so a renderer that
// draws a child after its parent gets the more specific hit.
func (h *hitmap) at(x, y int) (hitRegion, bool) {
	if h == nil {
		return hitRegion{}, false
	}
	for i := len(h.regions) - 1; i >= 0; i-- {
		if h.regions[i].contains(x, y) {
			return h.regions[i], true
		}
	}
	return hitRegion{}, false
}
