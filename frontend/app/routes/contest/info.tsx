import { useRevalidator } from "react-router";

import { useContest } from "./layout";
import { ConfirmButton } from "~/components/common";
import { Markdown } from "~/components/markdown";
import { Button } from "~/components/ui/button";
import { Card, CardContent } from "~/components/ui/card";
import { act, api } from "~/lib/api";
import { fmtDuration, fmtTime } from "~/lib/format";
import { useLanguageName, useSession } from "~/lib/session";

const REG_MODES = { invited: "By invitation", free: "Open registration", approval: "Registration with approval" };
const STATUS_TEXT: Record<string, string> = {
  requested: "Your registration is waiting for approval.",
  approved: "You are registered for this contest.",
  rejected: "Your registration was declined.",
  admin: "You manage this contest.",
};

export default function ContestInfoPage() {
  const { contest, access, description, participants } = useContest();
  const { account } = useSession();
  const revalidator = useRevalidator();
  const langName = useLanguageName();
  const base = `/contests/${contest.id}`;
  const run = async (path: string, method: string, message: string) => {
    await act(() => api(base + path, { method }), message);
    revalidator.revalidate();
  };

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem]">
      <Card>
        <CardContent>
          {description.trim() ? <Markdown>{description}</Markdown> : <p className="text-muted-foreground text-sm">No description.</p>}
        </CardContent>
      </Card>
      <aside className="grid content-start gap-4 text-sm">
        <Card>
          <CardContent className="grid gap-2">
            <Row label="Format">{contest.mode === "acm" ? "ICPC" : "IOI"}</Row>
            <Row label="Opens">{fmtTime(contest.start_at)}</Row>
            <Row label="Closes">{fmtTime(contest.end_at)}</Row>
            {contest.time_mode === "flexible" && <Row label="Your time">{fmtDuration(contest.duration_sec)} once started</Row>}
            <Row label="Registration">{REG_MODES[contest.reg_mode]}</Row>
            {contest.reg_mode !== "invited" && <Row label="Register by">{fmtTime(contest.reg_end_at)}</Row>}
            <Row label="Contestants">{participants}</Row>
            <Row label="Scoreboard">{contest.public_scoreboard ? "Public" : "Contestants only"}</Row>
            {contest.freeze_minutes > 0 && <Row label="Freeze">last {contest.freeze_minutes} min</Row>}
            {contest.mode === "acm" && <Row label="Penalty">{contest.penalty_minutes} min</Row>}
            {contest.cooldown_sec > 0 && <Row label="Submit every">{contest.cooldown_sec} s</Row>}
            {contest.languages.length > 0 && <Row label="Languages">{contest.languages.map(langName).join(", ")}</Row>}
          </CardContent>
        </Card>
        <Card>
          <CardContent className="grid gap-3">
            {!account && <p className="text-muted-foreground">Sign in to take part.</p>}
            {access.status && <p>{STATUS_TEXT[access.status]}</p>}
            {access.can_register && <Button onClick={() => run("/register", "POST", "Registered")}>Register</Button>}
            {access.can_withdraw && (
              <ConfirmButton title="Withdraw from this contest?" description="You can register again while registration is open." variant="outline" onConfirm={() => run("/register", "DELETE", "Withdrawn")}>
                Withdraw
              </ConfirmButton>
            )}
            {access.can_start && (
              <ConfirmButton
                title="Start your contest now?"
                description={`Your ${fmtDuration(contest.duration_sec)} timer starts immediately and cannot be paused.`}
                confirmLabel="Start"
                variant="default"
                size="default"
                onConfirm={() => run("/start", "POST", "Good luck!")}
              >
                Start contest
              </ConfirmButton>
            )}
            {access.is_participant && access.activated && contest.time_mode === "flexible" && (
              <p className="text-muted-foreground">
                Your window: {fmtTime(access.start)} – {fmtTime(access.end)}
              </p>
            )}
          </CardContent>
        </Card>
      </aside>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span className="text-muted-foreground shrink-0">{label}</span>
      <span className="text-right">{children}</span>
    </div>
  );
}
