import { Link } from "react-router";

import type { Route } from "./+types/problem";
import { ProblemView, type Problem } from "~/components/problem-view";
import { load } from "~/lib/api";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load<Problem>(`/contests/${params.cid}/problems/${params.id}`, request);
}

export default function ContestProblem({ loaderData: problem, params }: Route.ComponentProps) {
  return (
    <>
      <div className="mb-4 flex items-baseline gap-3">
        <Link to=".." relative="path" className="text-muted-foreground text-sm hover:underline">
          ← Problems
        </Link>
        <h2 className="truncate text-xl font-semibold">{problem.name}</h2>
      </div>
      <ProblemView problem={problem} contestId={Number(params.cid)} />
    </>
  );
}
