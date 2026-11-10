// Package contest contains the contest rules that do not touch storage:
// who may see or do what at a given time, and how scoreboards are computed.
package contest

import "time"

type Contest struct {
	ID               int       `json:"id"`
	Name             string    `json:"name"`
	CreatorID        *int      `json:"creator_id"`
	Mode             string    `json:"mode"`
	StartAt          time.Time `json:"start_at"`
	EndAt            time.Time `json:"end_at"`
	TimeMode         string    `json:"time_mode"`
	DurationSec      int       `json:"duration_sec"`
	RegMode          string    `json:"reg_mode"`
	RegEndAt         time.Time `json:"reg_end_at"`
	Languages        []string  `json:"languages"`
	PublicScoreboard bool      `json:"public_scoreboard"`
	AllowOtherPages  bool      `json:"allow_other_pages"`
	HideAdmin        bool      `json:"hide_admin"`
	CooldownSec      int       `json:"cooldown_sec"`
	FreezeMinutes    int       `json:"freeze_minutes"`
	KeepFrozen       bool      `json:"keep_frozen"`
	PenaltyMinutes   int       `json:"penalty_minutes"`
}

func (c *Contest) Flexible() bool { return c.TimeMode == "flexible" }

// Member is an account's row in a contest; nil means no relation at all.
type Member struct {
	Status       string
	SessionStart *time.Time
	SessionEnd   *time.Time
}

type Phase string

const (
	Before  Phase = "before"
	Running Phase = "running"
	Ended   Phase = "ended"
)

func phaseOf(start, end, now time.Time) Phase {
	switch {
	case !now.Before(end):
		return Ended
	case now.Before(start):
		return Before
	}
	return Running
}

// Scope describes which contest submissions a viewer may list.
type Scope string

const (
	ScopeNone         Scope = "none"
	ScopeOwn          Scope = "own"
	ScopeParticipants Scope = "participants" // approved contestants only, never admins
	ScopeAll          Scope = "all"
)

// Access is the resolved relation between one viewer and one contest at one
// instant. Handlers consult it instead of re-deriving timing rules.
type Access struct {
	AccountID int       `json:"-"`
	Phase     Phase     `json:"phase"`     // the viewer's own window
	Global    Phase     `json:"global"`    // the contest's configured window
	Status    string    `json:"status"`    // "", requested, approved, rejected, admin
	Activated bool      `json:"activated"` // false: flexible session not started yet
	Start     time.Time `json:"start"`     // viewer's effective window
	End       time.Time `json:"end"`

	IsAdmin       bool `json:"is_admin"`
	IsMember      bool `json:"is_member"`
	IsParticipant bool `json:"is_participant"`

	CanViewProblems   bool  `json:"can_view_problems"`
	CanSubmit         bool  `json:"can_submit"`
	CanViewScoreboard bool  `json:"can_view_scoreboard"`
	CanAsk            bool  `json:"can_ask"`
	CanStart          bool  `json:"can_start"`
	CanRegister       bool  `json:"can_register"`
	CanWithdraw       bool  `json:"can_withdraw"`
	Submissions       Scope `json:"submissions"`
	// RestrictResults: contest problems' result_style applies to this viewer.
	RestrictResults bool `json:"restrict_results"`
}

// Resolve computes the access of account acctID (0 for guests).
func Resolve(c *Contest, acctID int, m *Member, siteAdmin bool, now time.Time) Access {
	a := Access{AccountID: acctID, Activated: true, Start: c.StartAt, End: c.EndAt}
	a.Global = phaseOf(c.StartAt, c.EndAt, now)
	if m != nil {
		a.Status = m.Status
	}
	a.IsAdmin = acctID != 0 && (siteAdmin || a.Status == "admin" || (c.CreatorID != nil && *c.CreatorID == acctID))
	a.IsParticipant = a.Status == "approved" && !a.IsAdmin
	a.IsMember = a.IsAdmin || a.IsParticipant

	if c.Flexible() && a.IsParticipant {
		if m.SessionStart != nil && m.SessionEnd != nil {
			a.Start, a.End = *m.SessionStart, *m.SessionEnd
		} else {
			a.Activated = false
		}
	}
	if a.Activated {
		a.Phase = phaseOf(a.Start, a.End, now)
	} else {
		a.Phase = Before
	}

	// A flexible contestant whose own session is over must not see the set
	// again while others can still start theirs.
	endedView := a.Phase == Ended && (!(c.Flexible() && a.IsParticipant) || a.Global == Ended)
	if !a.Activated && a.Global == Ended {
		endedView = true
	}
	a.CanViewProblems = a.IsAdmin || endedView || (a.Phase == Running && a.IsMember)
	a.CanSubmit = a.IsMember && (a.IsAdmin || a.Phase == Running)

	available := a.Phase != Before
	if c.Flexible() && !a.IsAdmin {
		available = a.Global == Ended || (a.IsParticipant && a.Activated)
	}
	a.CanViewScoreboard = a.IsAdmin || (available && (c.PublicScoreboard || a.IsMember))

	a.CanAsk = a.IsParticipant && a.Global != Ended
	a.CanStart = c.Flexible() && a.IsParticipant && !a.Activated && a.Global == Running

	regOpen := now.Before(c.RegEndAt)
	a.CanRegister = acctID != 0 && !a.IsAdmin && c.RegMode != "invited" && regOpen && a.Status == ""
	a.CanWithdraw = !a.IsAdmin && (a.Status == "requested" || a.Status == "approved") &&
		regOpen && now.Before(c.StartAt)

	a.Submissions = a.scope(c)
	a.RestrictResults = !a.IsAdmin && a.Phase == Running
	return a
}

func (a Access) scope(c *Contest) Scope {
	if a.IsAdmin {
		return ScopeAll
	}
	phase := a.Phase
	if !a.Activated && a.Global == Ended {
		phase = Ended
	}
	switch {
	case phase == Before:
		return ScopeNone
	case phase == Running || !c.PublicScoreboard:
		if a.AccountID == 0 {
			return ScopeNone
		}
		return ScopeOwn
	}
	return ScopeParticipants
}

// CanViewSubmission reports whether the viewer may open one submission's
// result page. Source code is governed separately (owner and admins only).
func (a Access) CanViewSubmission(ownerID int, ownerIsAdmin bool) bool {
	switch a.Submissions {
	case ScopeAll:
		return true
	case ScopeOwn:
		return a.AccountID != 0 && ownerID == a.AccountID
	case ScopeParticipants:
		return !ownerIsAdmin || ownerID == a.AccountID
	}
	return false
}
