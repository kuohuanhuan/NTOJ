import type { Route } from "./+types/dashboard";
import { PageHeader } from "~/components/common";
import { Alert, AlertDescription, AlertTitle } from "~/components/ui/alert";
import { Card, CardContent } from "~/components/ui/card";
import { load } from "~/lib/api";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load("/admin/dashboard", request);
}

export default function Dashboard({ loaderData: d }: Route.ComponentProps) {
  const stats = [
    ["Accounts", d.accounts],
    ["Problems", d.problems],
    ["Contests", d.contests],
    ["Submissions", d.submissions],
    ["Queued", d.pending],
    ["Judging", d.judging],
    ["Open questions", d.open_questions],
  ];
  return (
    <>
      <PageHeader title="Dashboard" />
      {!d.judge_configured && (
        <Alert variant="destructive" className="mb-4">
          <AlertTitle>No judge is configured</AlertTitle>
          <AlertDescription>Set JUDGE_TOKEN on the server and connect a judge worker; until then submissions stay queued.</AlertDescription>
        </Alert>
      )}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        {stats.map(([label, value]) => (
          <Card key={label} className="py-4">
            <CardContent>
              <div className="text-muted-foreground text-xs">{label}</div>
              <div className="text-2xl font-semibold tabular-nums">{value}</div>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  );
}
