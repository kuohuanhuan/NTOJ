import { useRevalidator } from "react-router";

import type { Route } from "./+types/questions";
import { Empty, PageHeader, Pagination, UserLink } from "~/components/common";
import { Button } from "~/components/ui/button";
import { Card, CardContent } from "~/components/ui/card";
import { Textarea } from "~/components/ui/textarea";
import { act, api, load, qs } from "~/lib/api";
import { fmtTime } from "~/lib/format";

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load("/admin/questions" + qs({ page: new URL(request.url).searchParams.get("page") }), request);
}

export default function AdminQuestions({ loaderData }: Route.ComponentProps) {
  const revalidator = useRevalidator();
  return (
    <>
      <PageHeader title="Questions" description="Unanswered questions come first." />
      {loaderData.items.length === 0 && <Empty>No questions.</Empty>}
      <div className="grid gap-4">
        {loaderData.items.map((q: any) => (
          <Card key={q.id}>
            <CardContent className="grid gap-3 text-sm">
              <div>
                <div className="text-muted-foreground text-xs">
                  <UserLink id={q.account_id} name={q.account_name} /> (#{q.account_id}) · {fmtTime(q.created_at)}
                </div>
                <p className="break-words whitespace-pre-wrap">{q.content}</p>
              </div>
              <form
                className="grid gap-2"
                onSubmit={async (e) => {
                  e.preventDefault();
                  const reply = new FormData(e.currentTarget).get("reply");
                  if (await act(() => api(`/admin/questions/${q.id}/reply`, { body: { reply } }), "Reply sent")) revalidator.revalidate();
                }}
              >
                <Textarea name="reply" defaultValue={q.reply ?? ""} placeholder="Write a reply" maxLength={4000} required />
                <div>
                  <Button type="submit" size="sm">
                    {q.reply ? "Update reply" : "Reply"}
                  </Button>
                </div>
              </form>
            </CardContent>
          </Card>
        ))}
      </div>
      <Pagination total={loaderData.total} page={loaderData.page} perPage={loaderData.per_page} />
    </>
  );
}
