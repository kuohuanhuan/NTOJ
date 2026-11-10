import { useRevalidator } from "react-router";

import type { Route } from "./+types/questions";
import { ConfirmButton, Empty, PageHeader } from "~/components/common";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardFooter } from "~/components/ui/card";
import { Textarea } from "~/components/ui/textarea";
import { act, api, load } from "~/lib/api";
import { fmtTime } from "~/lib/format";

interface Question {
  id: number;
  content: string;
  reply: string | null;
  created_at: string;
  replied_at: string | null;
}

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<Question[]>("/questions", request);
}

export default function Questions({ loaderData }: Route.ComponentProps) {
  const revalidator = useRevalidator();
  const ask = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    if (await act(() => api("/questions", { body: { content: new FormData(form).get("content") } }), "Question sent")) {
      form.reset();
      revalidator.revalidate();
    }
  };
  return (
    <>
      <PageHeader title="Ask the administrators" description="Questions and replies here are private to you." />
      <form onSubmit={ask} className="mb-6 grid gap-2">
        <Textarea name="content" placeholder="What do you need help with?" maxLength={1024} required />
        <div>
          <Button type="submit">Send</Button>
        </div>
      </form>
      {loaderData.length === 0 && <Empty>You have not asked anything yet.</Empty>}
      <div className="grid gap-4">
        {loaderData.map((q) => (
          <Card key={q.id}>
            <CardContent className="grid gap-3 text-sm">
              <div>
                <div className="text-muted-foreground text-xs">You · {fmtTime(q.created_at)}</div>
                <p className="break-words whitespace-pre-wrap">{q.content}</p>
              </div>
              {q.reply ? (
                <div className="border-l-2 pl-3">
                  <div className="text-muted-foreground text-xs">Administrator · {fmtTime(q.replied_at)}</div>
                  <p className="break-words whitespace-pre-wrap">{q.reply}</p>
                </div>
              ) : (
                <p className="text-muted-foreground text-xs">Waiting for a reply.</p>
              )}
            </CardContent>
            <CardFooter>
              <ConfirmButton
                title="Delete this question?"
                variant="outline"
                onConfirm={async () => {
                  await act(() => api(`/questions/${q.id}`, { method: "DELETE" }));
                  revalidator.revalidate();
                }}
              >
                Delete
              </ConfirmButton>
            </CardFooter>
          </Card>
        ))}
      </div>
    </>
  );
}
