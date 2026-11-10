import type { Route } from "./+types/submissions";
import { SubmissionList, submissionQuery, type SubmissionPage } from "~/components/submission-table";
import { load } from "~/lib/api";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load<SubmissionPage>("/submissions" + submissionQuery(new URL(request.url), params.cid), request);
}

export default function ContestSubmissions({ loaderData, params }: Route.ComponentProps) {
  return <SubmissionList data={loaderData} contestId={Number(params.cid)} />;
}
