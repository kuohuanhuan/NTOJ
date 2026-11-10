// Package score turns per-testcase verdicts into subtask and total results.
package score

import (
	"math"
	"sort"
)

// Submission states. The numbering is part of the stored data format.
const (
	AC       = 1
	PC       = 2
	WA       = 3
	RE       = 4
	RESIG    = 5
	TLE      = 6
	MLE      = 7
	OLE      = 8
	CE       = 9
	CLE      = 10
	IE       = 11
	JE       = 12
	Judging  = 100
	Pending  = 101
	Skipped  = 102
	Rejected = 103
)

// ValidVerdict reports whether a judge may report state for a testcase.
func ValidVerdict(state int) bool {
	return (state >= AC && state <= JE) || state == Skipped
}

// Finished reports whether a submission state is final.
func Finished(state int) bool { return state < Judging || state > Pending }

type Subtask struct {
	Idx         int     `json:"idx"`
	Score       float64 `json:"score"`
	TestcaseIDs []int   `json:"testcase_ids"`
	Depends     []int   `json:"depends"`
}

type CaseResult struct {
	ID       int     `json:"id"`
	Name     string  `json:"name"`
	State    int     `json:"state"`
	TimeMS   int64   `json:"time_ms"`
	MemoryKB int64   `json:"memory_kb"`
	Message  string  `json:"message"`
	Ratio    float64 `json:"ratio"` // fraction of the points earned when State is PC
}

type SubtaskResult struct {
	Idx      int     `json:"idx"`
	State    int     `json:"state"`
	Score    float64 `json:"score"`
	MaxScore float64 `json:"max_score"`
	TimeMS   int64   `json:"time_ms"`
	MemoryKB int64   `json:"memory_kb"`
}

type Result struct {
	State    int
	Score    float64
	TimeMS   int64
	MemoryKB int64
	Subtasks []SubtaskResult
}

// Round rounds v to the given number of decimal places.
func Round(v float64, precision int) float64 {
	p := math.Pow(10, float64(precision))
	return math.Round(v*p) / p
}

func ratio(c CaseResult) float64 {
	switch c.State {
	case AC:
		return 1
	case PC:
		return math.Max(0, math.Min(1, c.Ratio))
	}
	return 0
}

// Evaluate applies group-min scoring: a subtask earns its score multiplied by
// the lowest ratio among its testcases, and nothing when a subtask it depends
// on was not fully solved. A problem without subtasks is treated as a single
// 100-point subtask covering every reported testcase.
func Evaluate(subtasks []Subtask, cases []CaseResult, precision int) Result {
	byID := make(map[int]CaseResult, len(cases))
	for _, c := range cases {
		byID[c.ID] = c
	}
	if len(subtasks) == 0 {
		all := Subtask{Idx: 1, Score: 100}
		for _, c := range cases {
			all.TestcaseIDs = append(all.TestcaseIDs, c.ID)
		}
		subtasks = []Subtask{all}
	}
	subtasks = append([]Subtask(nil), subtasks...)
	sort.Slice(subtasks, func(i, j int) bool { return subtasks[i].Idx < subtasks[j].Idx })

	res := Result{State: AC}
	full := map[int]bool{}
	firstBad := 0
	var max float64
	for _, st := range subtasks {
		sr := SubtaskResult{Idx: st.Idx, State: AC, MaxScore: st.Score}
		min := 1.0
		for _, id := range st.TestcaseIDs {
			c, ok := byID[id]
			if !ok {
				c = CaseResult{ID: id, State: Skipped}
			}
			if r := ratio(c); r < min {
				min = r
			}
			if c.State != AC && sr.State == AC {
				sr.State = c.State
			}
			sr.TimeMS = maxInt(sr.TimeMS, c.TimeMS)
			sr.MemoryKB = maxInt(sr.MemoryKB, c.MemoryKB)
		}
		for _, dep := range st.Depends {
			if !full[dep] {
				min = 0
				if sr.State == AC {
					sr.State = Skipped
				}
			}
		}
		full[st.Idx] = min == 1 && sr.State == AC
		sr.Score = Round(st.Score*min, precision)
		if sr.State != AC && sr.State != Skipped && firstBad == 0 {
			firstBad = sr.State
		}
		max += st.Score
		res.Score += sr.Score
		res.TimeMS = maxInt(res.TimeMS, sr.TimeMS)
		res.MemoryKB = maxInt(res.MemoryKB, sr.MemoryKB)
		res.Subtasks = append(res.Subtasks, sr)
	}
	res.Score = Round(res.Score, precision)
	allFull := true
	for _, st := range subtasks {
		allFull = allFull && full[st.Idx]
	}
	switch {
	case allFull:
		res.State = AC
	case res.Score > 0 && res.Score < max:
		res.State = PC
	case firstBad != 0 && firstBad != PC:
		res.State = firstBad
	default:
		res.State = WA
	}
	return res
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
