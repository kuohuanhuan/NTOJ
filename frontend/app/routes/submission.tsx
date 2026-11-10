import { useEffect } from "react";
import { Link, useRevalidator } from "react-router";

import type { Route } from "./+types/submission";
import { PageHeader, StateText, UserLink } from "~/components/common";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { act, api, load } from "~/lib/api";
import { fmtMemory, fmtTime, isFinal } from "~/lib/format";
import { useLanguageName } from "~/lib/session";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load(`/submissions/${params.id}`, request);
}

export function meta({ params }: Route.MetaArgs) {
  return [{ title: `Submission #${params.id}` }];
}

export default function Submission({ loaderData: s }: Route.ComponentProps) {
  const revalidator = useRevalidator();
  const langName = useLanguageName();
  const pending = !isFinal(s.state);

  // Poll while the judge is still working on it.
  useEffect(() => {
    if (!pending) return;
    const timer = setInterval(() => {
      if (document.visibilityState === "visible" && revalidator.state === "idle") revalidator.revalidate();
    }, 2000);
    return () => clearInterval(timer);
  }, [pending, revalidator]);

  const problemHref = s.contest_id ? `/contests/${s.contest_id}/problems/${s.problem_id}` : `/problems/${s.problem_id}`;
  const rejudge = async () => {
    if (await act(() => api(`/submissions/${s.id}/rejudge`, { method: "POST" }), "Queued for rejudge")) revalidator.revalidate();
  };

  return (
    <>
      <PageHeader
        title={`Submission #${s.id}`}
        description={
          s.contest_id && (
            <Link to={`/contests/${s.contest_id}/submissions`} className="hover:underline">
              ← Back to the contest
            </Link>
          )
        }
      >
        {s.can_rejudge && (
          <Button variant="outline" size="sm" onClick={rejudge}>
            Rejudge
          </Button>
        )}
      </PageHeader>
      <div className="grid gap-6">
        <Card>
          <CardContent className="grid grid-cols-2 gap-x-6 gap-y-3 text-sm sm:grid-cols-4">
            <Info label="Result">
              <StateText state={s.state} long className="text-base" />
            </Info>
            <Info label="Score">{s.score}</Info>
            <Info label="Time">{s.time_ms} ms</Info>
            <Info label="Memory">{fmtMemory(s.memory_kb)}</Info>
            <Info label="Problem">
              <Link to={problemHref} className="hover:underline">
                {s.problem_id}. {s.problem_name}
              </Link>
            </Info>
            <Info label="User">
              <UserLink id={s.account_id} name={s.account_name} />
            </Info>
            <Info label="Language">{langName(s.language)}</Info>
            <Info label="Submitted">{fmtTime(s.created_at)}</Info>
          </CardContent>
        </Card>

        {s.message && (
          <Card>
            <CardHeader>
              <CardTitle>Judge message</CardTitle>
            </CardHeader>
            <CardContent>
              <pre className="bg-muted max-h-96 overflow-auto rounded-md p-3 font-mono text-xs whitespace-pre-wrap">{s.message}</pre>
            </CardContent>
          </Card>
        )}

        {s.result.subtasks?.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Subtasks</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>#</TableHead>
                    <TableHead>Result</TableHead>
                    <TableHead className="text-right">Score</TableHead>
                    <TableHead className="text-right">Time</TableHead>
                    <TableHead className="text-right">Memory</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {s.result.subtasks.map((st: any) => (
                    <TableRow key={st.idx}>
                      <TableCell>{st.idx}</TableCell>
                      <TableCell>
                        <StateText state={st.state} long />
                      </TableCell>
                      <TableCell className="text-right tabular-nums">
                        {st.score} / {st.max_score}
                      </TableCell>
                      <TableCell className="text-right tabular-nums">{st.time_ms} ms</TableCell>
                      <TableCell className="text-right tabular-nums">{fmtMemory(st.memory_kb)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        )}

        {s.result.testcases?.length > 0 && (
          <Card>
            <CardHeader>
              <CardTitle>Test cases</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Result</TableHead>
                    <TableHead className="text-right">Time</TableHead>
                    <TableHead className="text-right">Memory</TableHead>
                    <TableHead>Message</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {s.result.testcases.map((tc: any) => (
                    <TableRow key={tc.id}>
                      <TableCell>{tc.name}</TableCell>
                      <TableCell>
                        <StateText state={tc.state} long />
                      </TableCell>
                      <TableCell className="text-right tabular-nums">{tc.time_ms} ms</TableCell>
                      <TableCell className="text-right tabular-nums">{fmtMemory(tc.memory_kb)}</TableCell>
                      <TableCell className="text-muted-foreground max-w-80 truncate font-mono text-xs" title={tc.message}>
                        {tc.message}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        )}
        {s.result_style !== "full" && (
          <p className="text-muted-foreground text-sm">Detailed results for this problem are hidden while the contest is running.</p>
        )}

        {s.code != null && (
          <Card>
            <CardHeader className="flex-row items-center justify-between">
              <CardTitle>Source code</CardTitle>
              <Button variant="outline" size="sm" onClick={() => act(() => navigator.clipboard.writeText(s.code), "Copied")}>
                Copy
              </Button>
            </CardHeader>
            <CardContent>
              <pre className="bg-muted max-h-[40rem] overflow-auto rounded-md p-3 font-mono text-xs">{s.code}</pre>
            </CardContent>
          </Card>
        )}
      </div>
    </>
  );
}

function Info({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-muted-foreground text-xs">{label}</div>
      <div className="truncate">{children}</div>
    </div>
  );
}
