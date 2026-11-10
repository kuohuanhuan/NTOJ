import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { Download } from "lucide-react";

import { Field, StateText } from "~/components/common";
import { Markdown } from "~/components/markdown";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { NativeSelect, NativeSelectOption } from "~/components/ui/native-select";
import { Textarea } from "~/components/ui/textarea";
import { act, api } from "~/lib/api";
import { fmtMemory, fmtSize } from "~/lib/format";
import { useSession } from "~/lib/session";

export interface Problem {
  id: number;
  name: string;
  status: string;
  allow_submit: boolean;
  tags: string[];
  statement: string;
  time_limit_ms: number;
  memory_limit_kb: number;
  checker: string;
  languages: string[];
  testcase_count: number;
  subtasks: { idx: number; score: number; testcases: number; depends: number[] }[];
  files: { name: string; size: number }[];
  stats?: { submissions: number; ac_submissions: number; tried_users: number; solved_users: number; my_state: number | null; my_score: number | null };
  can_submit?: boolean;
}

function SubmitForm({ problem, contestId }: { problem: Problem; contestId?: number }) {
  const { site, account } = useSession();
  const navigate = useNavigate();
  const languages = site.languages.filter((l) => problem.languages.length === 0 || problem.languages.includes(l.id));
  const preferred = languages.find((l) => l.id === account?.last_language) ?? languages[0];
  const [language, setLanguage] = useState(preferred?.id ?? "");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);

  if (!account) {
    return (
      <p className="text-muted-foreground text-sm">
        <Link to="/signin" className="text-foreground underline">
          Sign in
        </Link>{" "}
        to submit a solution.
      </p>
    );
  }
  if (languages.length === 0) return <p className="text-muted-foreground text-sm">No language is available for this problem.</p>;

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    let id = 0;
    const ok = await act(async () => {
      const res = await api<{ id: number }>("/submissions", {
        body: { problem_id: problem.id, contest_id: contestId ?? null, language, code },
      });
      id = res.id;
    });
    setBusy(false);
    if (ok) navigate(`/submissions/${id}`);
  };

  const loadFile = async (file: File | undefined) => {
    if (!file) return;
    if (file.size > 64 * 1024) return void act(() => Promise.reject(new Error("Source files are limited to 64 KiB")));
    setCode(await file.text());
  };

  return (
    <form onSubmit={submit} className="grid gap-3">
      <div className="flex flex-wrap items-end gap-3">
        <Field label="Language">
          <NativeSelect value={language} onChange={(e) => setLanguage(e.target.value)}>
            {languages.map((l) => (
              <NativeSelectOption key={l.id} value={l.id}>
                {l.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </Field>
        <Field label="Or upload a source file">
          <input type="file" className="text-sm" onChange={(e) => loadFile(e.target.files?.[0])} />
        </Field>
      </div>
      <Textarea
        value={code}
        onChange={(e) => setCode(e.target.value)}
        placeholder="Paste your source code here"
        spellCheck={false}
        required
        className="max-h-[32rem] min-h-56 font-mono text-xs md:text-xs"
      />
      <div>
        <Button type="submit" disabled={busy || code.trim() === ""}>
          Submit
        </Button>
      </div>
    </form>
  );
}

/** Statement, limits, attachments and the submit form for one problem. */
export function ProblemView({ problem, contestId }: { problem: Problem; contestId?: number }) {
  const canSubmit = problem.allow_submit && (contestId === undefined || problem.can_submit);
  const total = problem.subtasks.reduce((sum, s) => sum + s.score, 0);
  const submissionsHref = contestId
    ? `/contests/${contestId}/submissions?problem_id=${problem.id}`
    : `/submissions?problem_id=${problem.id}`;

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_17rem]">
      <div className="grid min-w-0 gap-6">
        <Card>
          <CardContent>
            {problem.statement.trim() ? (
              <Markdown>{problem.statement}</Markdown>
            ) : (
              <p className="text-muted-foreground text-sm">This problem has no statement yet.</p>
            )}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Submit</CardTitle>
          </CardHeader>
          <CardContent>
            {canSubmit ? (
              <SubmitForm problem={problem} contestId={contestId} />
            ) : (
              <p className="text-muted-foreground text-sm">Submissions are closed for this problem.</p>
            )}
          </CardContent>
        </Card>
      </div>
      <aside className="grid content-start gap-4 text-sm">
        <Card>
          <CardContent className="grid gap-2">
            <Row label="Time limit">{problem.time_limit_ms} ms</Row>
            <Row label="Memory limit">{fmtMemory(problem.memory_limit_kb)}</Row>
            <Row label="Checker">{problem.checker}</Row>
            <Row label="Test cases">{problem.testcase_count}</Row>
            {problem.stats && (
              <>
                <Row label="Solved by">
                  {problem.stats.solved_users} / {problem.stats.tried_users}
                </Row>
                <Row label="Your result">
                  <StateText state={problem.stats.my_state} />
                  {problem.stats.my_score != null && <span className="text-muted-foreground ml-1">({problem.stats.my_score})</span>}
                </Row>
              </>
            )}
            {problem.tags.length > 0 && (
              <div className="flex flex-wrap gap-1 pt-1">
                {problem.tags.map((t) => (
                  <Badge key={t} variant="secondary">
                    {t}
                  </Badge>
                ))}
              </div>
            )}
            <div className="flex flex-wrap gap-2 pt-2">
              <Button asChild variant="outline" size="sm">
                <Link to={submissionsHref}>Submissions</Link>
              </Button>
              {contestId === undefined && (
                <Button asChild variant="outline" size="sm">
                  <Link to={`/problems/${problem.id}/rank`}>Ranking</Link>
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
        {problem.subtasks.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Subtasks</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-1.5">
              {problem.subtasks.map((s) => (
                <Row key={s.idx} label={`#${s.idx}${s.depends.length ? ` (needs ${s.depends.map((d) => "#" + d).join(", ")})` : ""}`}>
                  {s.score} pts · {s.testcases} tests
                </Row>
              ))}
              <Row label="Total">{total} pts</Row>
            </CardContent>
          </Card>
        )}
        {problem.files.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Attachments</CardTitle>
            </CardHeader>
            <CardContent className="grid gap-1.5">
              {problem.files.map((f) => (
                <a
                  key={f.name}
                  href={`/api/problems/${problem.id}/files/${encodeURIComponent(f.name)}`}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="flex items-center gap-2 hover:underline"
                >
                  <Download className="size-3.5 shrink-0" />
                  <span className="truncate">{f.name}</span>
                  <span className="text-muted-foreground ml-auto shrink-0 text-xs">{fmtSize(f.size)}</span>
                </a>
              ))}
            </CardContent>
          </Card>
        )}
      </aside>
    </div>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span className="text-muted-foreground">{label}</span>
      <span className="text-right">{children}</span>
    </div>
  );
}
