import type { Route } from "./+types/rank";
import { PageHeader, Pagination, UserLink } from "~/components/common";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { load, qs } from "~/lib/api";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load("/rank" + qs({ page: new URL(request.url).searchParams.get("page") }), request);
}

export default function Rank({ loaderData }: Route.ComponentProps) {
  return (
    <>
      <PageHeader title="Ranking" description="Sum of the best score on every public problem." />
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-16">Rank</TableHead>
            <TableHead>User</TableHead>
            <TableHead>Motto</TableHead>
            <TableHead className="text-right">Solved</TableHead>
            <TableHead className="text-right">Score</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loaderData.items.map((u: any) => (
            <TableRow key={u.id}>
              <TableCell className="tabular-nums">{u.rank}</TableCell>
              <TableCell className="max-w-56 truncate font-medium">
                <UserLink id={u.id} name={u.name} />
              </TableCell>
              <TableCell className="text-muted-foreground max-w-96 truncate">{u.motto}</TableCell>
              <TableCell className="text-right tabular-nums">{u.ac_problems}</TableCell>
              <TableCell className="text-right tabular-nums">{u.rate}</TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <Pagination total={loaderData.total} page={loaderData.page} perPage={loaderData.per_page} />
    </>
  );
}
