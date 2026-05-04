package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

type stats struct {
	Count       int            `json:"count"`
	WireBytes   int64          `json:"wire_bytes"`
	DurationMs  int64          `json:"duration_ms"`
	DDPTypeHist map[uint8]int  `json:"ddp_types"`
	ATPFuncHist map[string]int `json:"atp_funcs"`
	Notes       map[string]int `json:"notes"`
}

func summarize(events []Event) stats {
	s := stats{
		DDPTypeHist: map[uint8]int{},
		ATPFuncHist: map[string]int{},
		Notes:       map[string]int{},
	}
	if len(events) == 0 {
		return s
	}
	s.Count = len(events)
	first, last := events[0].Timestamp, events[0].Timestamp
	for _, e := range events {
		s.WireBytes += int64(e.WireLen)
		if e.Note != "" {
			s.Notes[e.Note]++
			continue
		}
		s.DDPTypeHist[e.DDPType]++
		if e.ATPFunc != "" {
			s.ATPFuncHist[e.ATPFunc]++
		}
		if e.Timestamp.Before(first) {
			first = e.Timestamp
		}
		if e.Timestamp.After(last) {
			last = e.Timestamp
		}
	}
	s.DurationMs = last.Sub(first).Milliseconds()
	return s
}

type report struct {
	Left      string  `json:"left"`
	Right     string  `json:"right"`
	LeftStat  stats   `json:"left_stats"`
	RightStat stats   `json:"right_stats"`
	Timeline  [][]any `json:"timeline,omitempty"`
}

func renderJSON(w io.Writer, lpath, rpath string, left, right []Event, limit int) {
	r := report{
		Left:      lpath,
		Right:     rpath,
		LeftStat:  summarize(left),
		RightStat: summarize(right),
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
	_ = limit // JSON output already returns full event arrays via the public Event slice if needed.
}

func renderText(w io.Writer, lpath, rpath string, left, right []Event, limit int) {
	ls := summarize(left)
	rs := summarize(right)

	fmt.Fprintf(w, "pcapdiff\n  left:  %s (%d packets, %d bytes, %d ms)\n  right: %s (%d packets, %d bytes, %d ms)\n\n",
		lpath, ls.Count, ls.WireBytes, ls.DurationMs,
		rpath, rs.Count, rs.WireBytes, rs.DurationMs)

	fmt.Fprintln(w, "DDP type histogram:")
	printIntHist(w, ls.DDPTypeHist, rs.DDPTypeHist, func(k uint8) string { return fmt.Sprintf("type=%d", k) })

	fmt.Fprintln(w, "\nATP function histogram:")
	printStrHist(w, ls.ATPFuncHist, rs.ATPFuncHist)

	if len(ls.Notes) > 0 || len(rs.Notes) > 0 {
		fmt.Fprintln(w, "\nDecode notes (non-DDP / errors):")
		printStrHist(w, ls.Notes, rs.Notes)
	}

	fmt.Fprintln(w, "\nTimeline (relative ms within each capture):")
	fmt.Fprintf(w, "  %-6s %-9s %-9s %-7s %-5s %-7s %-9s   |   %-6s %-9s %-9s %-7s %-5s %-7s %-9s\n",
		"#", "src", "dst", "ddp", "atp", "tid", "note",
		"#", "src", "dst", "ddp", "atp", "tid", "note")
	n := max(len(left), len(right))
	if limit > 0 && n > limit {
		n = limit
	}
	for i := 0; i < n; i++ {
		writeRow(w, i, left, right)
	}
}

func writeRow(w io.Writer, i int, left, right []Event) {
	fmt.Fprintf(w, "  %s   |   %s\n", fmtCell(i, left), fmtCell(i, right))
}

func fmtCell(i int, evs []Event) string {
	if i >= len(evs) {
		return fmt.Sprintf("%-6s %-9s %-9s %-7s %-5s %-7s %-9s", "-", "", "", "", "", "", "")
	}
	e := evs[i]
	relMs := int64(0)
	if len(evs) > 0 {
		relMs = e.Timestamp.Sub(evs[0].Timestamp).Milliseconds()
	}
	tid := ""
	if e.ATPFunc != "" {
		tid = fmt.Sprintf("%d", e.ATPTID)
	}
	ddpStr := ""
	if e.Note == "" {
		ddpStr = fmt.Sprintf("%d", e.DDPType)
	}
	note := e.Note
	if len(note) > 9 {
		note = note[:9]
	}
	return fmt.Sprintf("%-6d %-9s %-9s %-7s %-5s %-7s %-9s",
		relMs, e.Source, e.Dest, ddpStr, e.ATPFunc, tid, note)
}

func printIntHist(w io.Writer, l, r map[uint8]int, label func(uint8) string) {
	keys := map[uint8]struct{}{}
	for k := range l {
		keys[k] = struct{}{}
	}
	for k := range r {
		keys[k] = struct{}{}
	}
	ordered := make([]uint8, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	for _, k := range ordered {
		fmt.Fprintf(w, "  %-12s left=%-6d right=%-6d delta=%+d\n", label(k), l[k], r[k], r[k]-l[k])
	}
}

func printStrHist(w io.Writer, l, r map[string]int) {
	keys := map[string]struct{}{}
	for k := range l {
		keys[k] = struct{}{}
	}
	for k := range r {
		keys[k] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, k := range ordered {
		fmt.Fprintf(w, "  %-20s left=%-6d right=%-6d delta=%+d\n", k, l[k], r[k], r[k]-l[k])
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
