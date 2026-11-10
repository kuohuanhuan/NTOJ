import { Form, Link, useSearchParams } from "react-router";

import { Empty, Pagination, StateText, UserLink } from "~/components/common";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { NativeSelect, NativeSelectOption } from "~/components/ui/native-select";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { fmtMemory, fmtTime, STATES } from "~/lib/format";
import { useLanguageName, useSession } from "~/lib/session";

export interface SubmissionRow {
  id: number;
  problem_id: number;
  problem_name: string;
  account_id: number;
  account_name: string;
  contest_id: number | null;
  language: string;
  created_at: string;
  state: number;
  score: number;
  time_ms: number;
  memory_kb: number;
}

export interface SubmissionPage {
  items: SubmissionRow[];
  total: number;
  page: number;
  per_page: number;
}

/** Translates the page's search parameters into the API's submission filters. */
export function submissionQuery(url: URL, contestId?: string): string {
  const out = new URLSearchParams();
  for (const key of ["problem_id", "account_id", "state", "language", "page"]) {
    const v = url.searchParams.get(key);
    if (v) out.set(key, v);
  }
  if (contestId) out.set("contest_id", contestId);
  return "?" + out.toString();
}

export function SubmissionList({ data, contestId }: { data: SubmissionPage; contestId?: number }) {
  const [params] = useSearchParams();
  const { site } = useSession();
  const langName = useLanguageName();
  const problemHref = (id: number) => (contestId ? `/contests/${contestId}/problems/${id}` : `/problems/${id}`);

  return (
    <>
      <Form method="get" className="mb-4 flex flex-wrap items-end gap-2">
        <Input name="problem_id" placeholder="Problem ID" inputMode="numeric" pattern="\d*" defaultValue={params.get("problem_id") ?? ""} className="w-28" />
        <Input name="account_id" placeholder="User ID" inputMode="numeric" pattern="\d*" defaultValue={params.get("account_id") ?? ""} className="w-28" />
        <NativeSelect name="state" defaultValue={params.get("state") ?? ""} aria-label="Result">
          <NativeSelectOption value="">Any result</NativeSelectOption>
          {Object.entries(STATES).map(([id, s]) => (
            <NativeSelectOption key={id} value={id}>
              {s.long}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <NativeSelect name="language" defaultValue={params.get("language") ?? ""} aria-label="Language">
          <NativeSelectOption value="">Any language</NativeSelectOption>
          {site.languages.map((l) => (
            <NativeSelectOption key={l.id} value={l.id}>
              {l.name}
            </NativeSelectOption>
          ))}
        </NativeSelect>
        <Button type="submit" variant="secondary">
          Filter
        </Button>
      </Form>
      {data.items.length === 0 ? (
        <Empty>No submissions.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>#</TableHead>
              <TableHead>Problem</TableHead>
              <TableHead>User</TableHead>
              <TableHead>Result</TableHead>
              <TableHead className="text-right">Score</TableHead>
              <TableHead className="text-right">Time</TableHead>
              <TableHead className="text-right">Memory</TableHead>
              <TableHead>Language</TableHead>
              <TableHead>Submitted</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.items.map((s) => (
              <TableRow key={s.id}>
                <TableCell>
                  <Link to={`/submissions/${s.id}`} className="text-blue-600 hover:underline dark:text-blue-400">
                    {s.id}
                  </Link>
                </TableCell>
                <TableCell className="max-w-56 truncate">
                  <Link to={problemHref(s.problem_id)} className="hover:underline">
                    {s.problem_id}. {s.problem_name}
                  </Link>
                </TableCell>
                <TableCell className="max-w-40 truncate">
                  <UserLink id={s.account_id} name={s.account_name} />
                </TableCell>
                <TableCell>
                  <Link to={`/submissions/${s.id}`}>
                    <StateText state={s.state} />
                  </Link>
                </TableCell>
                <TableCell className="text-right tabular-nums">{s.score}</TableCell>
                <TableCell className="text-right tabular-nums">{s.time_ms} ms</TableCell>
                <TableCell className="text-right tabular-nums">{fmtMemory(s.memory_kb)}</TableCell>
                <TableCell>{langName(s.language)}</TableCell>
                <TableCell className="text-muted-foreground">{fmtTime(s.created_at)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <Pagination total={data.total} page={data.page} perPage={data.per_page} />
    </>
  );
}
