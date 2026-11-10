package score

import "testing"

func cases(states ...int) []CaseResult {
	out := make([]CaseResult, len(states))
	for i, s := range states {
		out[i] = CaseResult{ID: i + 1, State: s, TimeMS: int64(10 * (i + 1)), MemoryKB: 100}
	}
	return out
}

func TestEvaluateWithoutSubtasks(t *testing.T) {
	r := Evaluate(nil, cases(AC, AC), 0)
	if r.State != AC || r.Score != 100 || r.TimeMS != 20 {
		t.Fatalf("all AC: %+v", r)
	}
	r = Evaluate(nil, cases(AC, TLE), 0)
	if r.State != TLE || r.Score != 0 {
		t.Fatalf("one TLE: %+v", r)
	}
}

func TestEvaluateGroupMinAndDependencies(t *testing.T) {
	subtasks := []Subtask{
		{Idx: 1, Score: 30, TestcaseIDs: []int{1}},
		{Idx: 2, Score: 70, TestcaseIDs: []int{2, 3}, Depends: []int{1}},
	}
	r := Evaluate(subtasks, cases(AC, AC, WA), 0)
	if r.State != PC || r.Score != 30 || r.Subtasks[1].State != WA || r.Subtasks[1].Score != 0 {
		t.Fatalf("partial: %+v", r)
	}
	// Failing the dependency voids the dependent subtask even if its own cases pass.
	r = Evaluate(subtasks, cases(RE, AC, AC), 0)
	if r.State != RE || r.Score != 0 || r.Subtasks[1].State != Skipped {
		t.Fatalf("dependency: %+v", r)
	}
	r = Evaluate(subtasks, cases(AC, AC, AC), 0)
	if r.State != AC || r.Score != 100 {
		t.Fatalf("full: %+v", r)
	}
}

func TestEvaluatePartialRatioAndMissingCase(t *testing.T) {
	subtasks := []Subtask{{Idx: 1, Score: 10, TestcaseIDs: []int{1, 2}}}
	cs := cases(AC, PC)
	cs[1].Ratio = 0.333
	r := Evaluate(subtasks, cs, 2)
	if r.State != PC || r.Score != 3.33 {
		t.Fatalf("ratio: %+v", r)
	}
	cs[1].Ratio = 7 // out-of-range ratios are clamped
	if r := Evaluate(subtasks, cs, 2); r.Score != 10 || r.State == AC {
		t.Fatalf("clamp: %+v", r)
	}
	// A testcase the judge never reported counts as skipped, not as passed.
	r = Evaluate(subtasks, cases(AC), 0)
	if r.Score != 0 || r.State == AC {
		t.Fatalf("missing: %+v", r)
	}
}
