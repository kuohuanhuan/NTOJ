import { Link } from "react-router";

import type { Route } from "./+types/problem";
import { PageHeader } from "~/components/common";
import { ProblemView, type Problem } from "~/components/problem-view";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { load } from "~/lib/api";
import { useSession } from "~/lib/session";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load<Problem>(`/problems/${params.id}`, request);
}

export function meta({ loaderData: data }: Route.MetaArgs) {
  return [{ title: data ? `${data.id}. ${data.name}` : "Problem" }];
}

export default function ProblemPage({ loaderData: problem }: Route.ComponentProps) {
  const { account } = useSession();
  return (
    <>
      <PageHeader
        title={
          <>
            <span className="text-muted-foreground mr-2">#{problem.id}</span>
            {problem.name}
            {problem.status !== "online" && (
              <Badge variant="outline" className="ml-2 align-middle">
                {problem.status}
              </Badge>
            )}
          </>
        }
      >
        {account?.role === "admin" && (
          <Button asChild variant="outline" size="sm">
            <Link to={`/admin/problems/${problem.id}`}>Manage</Link>
          </Button>
        )}
      </PageHeader>
      <ProblemView problem={problem} />
    </>
  );
}
