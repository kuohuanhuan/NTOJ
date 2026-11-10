import { Link } from "react-router";

import type { Route } from "./+types/boards";
import { Empty, PageHeader } from "~/components/common";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { load } from "~/lib/api";
import { fmtTime } from "~/lib/format";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<{ id: number; name: string; start_at: string; end_at: string }[]>("/boards", request);
}

export default function Boards({ loaderData }: Route.ComponentProps) {
  return (
    <>
      <PageHeader title="Boards" description="Standings over a set of problems for a group of users." />
      {loaderData.length === 0 ? (
        <Empty>No boards.</Empty>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>From</TableHead>
              <TableHead>To</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loaderData.map((b) => (
              <TableRow key={b.id}>
                <TableCell className="font-medium">
                  <Link to={`/boards/${b.id}`} className="hover:underline">
                    {b.name}
                  </Link>
                </TableCell>
                <TableCell>{fmtTime(b.start_at)}</TableCell>
                <TableCell>{fmtTime(b.end_at)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </>
  );
}
