package contest

import (
	"sort"
	"time"

	"github.com/kuohuanhuan/ntoj/backend/internal/score"
)

type BoardProblem struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ScoreType string `json:"score_type"`
}

type BoardUser struct {
	ID    int
	Name  string
	Start time.Time
	End   time.Time
}

type BoardSubmission struct {
	ID        int64
	AccountID int
	ProblemID int
	At        time.Time
	State     int
	Score     float64
	Subtasks  []score.SubtaskResult
}

type Cell struct {
	Score    float64 `json:"score"`
	Attempts int     `json:"attempts"` // judged attempts (ACM: rejected ones before the first AC)
	Solved   bool    `json:"solved"`
	Minutes  int     `json:"minutes"` // ACM: minutes from the account's start to the first AC
	Frozen   int     `json:"frozen"`  // attempts hidden by the scoreboard freeze
	Pending  int     `json:"pending"`
}

type Row struct {
	Rank      int           `json:"rank"`
	AccountID int           `json:"account_id"`
	Name      string        `json:"name"`
	Total     float64       `json:"total"`
	Solved    int           `json:"solved"`
	Penalty   int           `json:"penalty"`
	Cells     map[int]*Cell `json:"cells"`
}

// BoardOptions configure one scoreboard computation.
type BoardOptions struct {
	Mode           string // "ioi" or "acm"
	PenaltyMinutes int
	// FreezeMinutes > 0 hides submissions made in the last minutes of each
	// account's window. Zero disables the freeze.
	FreezeMinutes int
}

// Scoreboard ranks users. subs must be ordered by ID ascending. Submissions
// outside an account's own window are ignored.
func Scoreboard(opt BoardOptions, problems []BoardProblem, users []BoardUser, subs []BoardSubmission) []Row {
	rows := make(map[int]*Row, len(users))
	window := make(map[int]BoardUser, len(users))
	ptype := make(map[int]string, len(problems))
	for _, p := range problems {
		ptype[p.ID] = p.ScoreType
	}
	for _, u := range users {
		rows[u.ID] = &Row{AccountID: u.ID, Name: u.Name, Cells: map[int]*Cell{}}
		window[u.ID] = u
	}
	union := map[[2]int]map[int]float64{} // (account, problem) -> subtask idx -> best score

	for _, s := range subs {
		row, ok := rows[s.AccountID]
		st, isProblem := ptype[s.ProblemID]
		if !ok || !isProblem {
			continue
		}
		u := window[s.AccountID]
		if s.At.Before(u.Start) || !s.At.Before(u.End) {
			continue
		}
		cell := row.Cells[s.ProblemID]
		if cell == nil {
			cell = &Cell{}
			row.Cells[s.ProblemID] = cell
		}
		if opt.FreezeMinutes > 0 && !s.At.Before(u.End.Add(-time.Duration(opt.FreezeMinutes)*time.Minute)) {
			cell.Frozen++
			continue
		}
		if !score.Finished(s.State) {
			cell.Pending++
			continue
		}
		if opt.Mode == "acm" {
			if cell.Solved || s.State >= score.CE { // compile and judge errors are free
				continue
			}
			if s.State == score.AC {
				cell.Solved = true
				cell.Minutes = int(s.At.Sub(u.Start) / time.Minute)
				row.Solved++
				row.Penalty += cell.Minutes + cell.Attempts*opt.PenaltyMinutes
			}
			cell.Attempts++
			continue
		}
		cell.Attempts++
		if s.State == score.AC {
			cell.Solved = true
		}
		if st == "subtask_union" {
			key := [2]int{s.AccountID, s.ProblemID}
			if union[key] == nil {
				union[key] = map[int]float64{}
			}
			var sum float64
			for _, sr := range s.Subtasks {
				if sr.Score > union[key][sr.Idx] {
					union[key][sr.Idx] = sr.Score
				}
			}
			for _, v := range union[key] {
				sum += v
			}
			cell.Score = score.Round(sum, 3)
		} else if s.Score > cell.Score {
			cell.Score = s.Score
		}
	}

	out := make([]Row, 0, len(rows))
	for _, u := range users {
		row := rows[u.ID]
		for _, c := range row.Cells {
			row.Total += c.Score
		}
		row.Total = score.Round(row.Total, 3)
		out = append(out, *row)
	}
	less := func(a, b *Row) bool {
		if opt.Mode == "acm" {
			if a.Solved != b.Solved {
				return a.Solved > b.Solved
			}
			return a.Penalty < b.Penalty
		}
		return a.Total > b.Total
	}
	sort.SliceStable(out, func(i, j int) bool {
		if less(&out[i], &out[j]) || less(&out[j], &out[i]) {
			return less(&out[i], &out[j])
		}
		return out[i].AccountID < out[j].AccountID
	})
	for i := range out {
		out[i].Rank = i + 1
		if i > 0 && !less(&out[i-1], &out[i]) {
			out[i].Rank = out[i-1].Rank
		}
	}
	return out
}
