import type { Route } from "./+types/submissions";
import { PageHeader } from "~/components/common";
import { SubmissionList, submissionQuery, type SubmissionPage } from "~/components/submission-table";
import { load } from "~/lib/api";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<SubmissionPage>("/submissions" + submissionQuery(new URL(request.url)), request);
}

export default function Submissions({ loaderData }: Route.ComponentProps) {
  return (
    <>
      <PageHeader title="Submissions" />
      <SubmissionList data={loaderData} />
    </>
  );
}
