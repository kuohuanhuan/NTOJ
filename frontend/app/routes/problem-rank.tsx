import { Link } from "react-router";

import type { Route } from "./+types/problem-rank";
import { Empty, PageHeader, Pagination, UserLink } from "~/components/common";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { load, qs } from "~/lib/api";
import { fmtMemory, fmtTime } from "~/lib/format";
import { useLanguageName } from "~/lib/session";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  const page = new URL(request.url).searchParams.get("page");
  return load(`/problems/${params.id}/rank` + qs({ page }), request);
}

export default function ProblemRank({ loaderData, params }: Route.ComponentProps) {
  const langName = useLanguageName();
  const offset = (loaderData.page - 1) * loaderData.per_page;
  return (
    <>
      <PageHeader
        title={`Problem #${params.id} ranking`}
        description={
          <Link to={`/problems/${params.id}`} className="hover:underline">
            ← Back to the problem
          </Link>
        }
      />
      {loaderData.items.length === 0 ? (
        <Empty>Nobody has solved this problem yet.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-16">Rank</TableHead>
              <TableHead>User</TableHead>
              <TableHead className="text-right">Time</TableHead>
              <TableHead className="text-right">Memory</TableHead>
              <TableHead>Language</TableHead>
              <TableHead>Submission</TableHead>
              <TableHead>Submitted</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loaderData.items.map((r: any, i: number) => (
              <TableRow key={r.id}>
                <TableCell className="tabular-nums">{offset + i + 1}</TableCell>
                <TableCell>
                  <UserLink id={r.account_id} name={r.account_name} />
                </TableCell>
                <TableCell className="text-right tabular-nums">{r.time_ms} ms</TableCell>
                <TableCell className="text-right tabular-nums">{fmtMemory(r.memory_kb)}</TableCell>
                <TableCell>{langName(r.language)}</TableCell>
                <TableCell>
                  <Link to={`/submissions/${r.id}`} className="text-blue-600 hover:underline dark:text-blue-400">
                    {r.id}
                  </Link>
                </TableCell>
                <TableCell className="text-muted-foreground">{fmtTime(r.created_at)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <Pagination total={loaderData.total} page={loaderData.page} perPage={loaderData.per_page} />
    </>
  );
}
