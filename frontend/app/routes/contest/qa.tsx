import { useState } from "react";
import { useRevalidator } from "react-router";

import type { Route } from "./+types/qa";
import { ConfirmButton, Empty, Field } from "~/components/common";
import { Markdown } from "~/components/markdown";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import { act, api, load } from "~/lib/api";
import { fmtTime } from "~/lib/format";

export async function clientLoader({ params, request }: Route.ClientLoaderArgs) {
  return load(`/contests/${params.cid}/qa`, request);
}

function PostForm({ onSubmit, initial, submitLabel }: { onSubmit: (subject: string, content: string) => Promise<boolean>; initial?: { subject: string; content: string }; submitLabel: string }) {
  const submit = async (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const form = e.currentTarget;
    const f = new FormData(form);
    if (await onSubmit(String(f.get("subject")), String(f.get("content")))) form.reset();
  };
  return (
    <form onSubmit={submit} className="grid gap-3">
      <Field label="Subject">
        <Input name="subject" maxLength={100} defaultValue={initial?.subject} required />
      </Field>
      <Field label="Content">
        <Textarea name="content" maxLength={4000} defaultValue={initial?.content} required />
      </Field>
      <div>
        <Button type="submit">{submitLabel}</Button>
      </div>
    </form>
  );
}

export default function ContestQA({ loaderData, params }: Route.ComponentProps) {
  const revalidator = useRevalidator();
  const [editing, setEditing] = useState<number | null>(null);
  const base = `/contests/${params.cid}`;
  const { announcements, questions, can_ask, is_admin } = loaderData;
  const done = (ok: boolean) => {
    if (ok) revalidator.revalidate();
    return ok;
  };

  return (
    <div className="grid gap-8 lg:grid-cols-2">
      <section className="grid content-start gap-4">
        <h2 className="text-lg font-semibold">Announcements</h2>
        {is_admin && (
          <Card>
            <CardContent>
              <PostForm
                submitLabel="Publish"
                onSubmit={async (subject, content) => done(await act(() => api(`${base}/manage/announcements`, { body: { subject, content } }), "Published"))}
              />
            </CardContent>
          </Card>
        )}
        {announcements.length === 0 && <Empty>No announcements.</Empty>}
        {announcements.map((n: any) => (
          <Card key={n.id}>
            <CardHeader>
              <CardTitle>{n.subject}</CardTitle>
              <CardDescription>
                {fmtTime(n.created_at)}
                {n.updated_at !== n.created_at && ` · edited ${fmtTime(n.updated_at)}`}
              </CardDescription>
            </CardHeader>
            <CardContent>
              {editing === n.id ? (
                <PostForm
                  initial={n}
                  submitLabel="Save"
                  onSubmit={async (subject, content) => {
                    const ok = done(await act(() => api(`${base}/manage/announcements/${n.id}`, { method: "PUT", body: { subject, content } }), "Saved"));
                    if (ok) setEditing(null);
                    return ok;
                  }}
                />
              ) : (
                <Markdown>{n.content}</Markdown>
              )}
            </CardContent>
            {is_admin && editing !== n.id && (
              <CardFooter>
                <Button variant="outline" size="sm" onClick={() => setEditing(n.id)}>
                  Edit
                </Button>
                <ConfirmButton title="Delete this announcement?" onConfirm={async () => done(await act(() => api(`${base}/manage/announcements/${n.id}`, { method: "DELETE" })))}>
                  Delete
                </ConfirmButton>
              </CardFooter>
            )}
          </Card>
        ))}
      </section>

      <section className="grid content-start gap-4">
        <h2 className="text-lg font-semibold">{is_admin ? "Questions from contestants" : "Your questions"}</h2>
        {can_ask && (
          <Card>
            <CardContent>
              <PostForm submitLabel="Ask" onSubmit={async (subject, content) => done(await act(() => api(`${base}/questions`, { body: { subject, content } }), "Question sent"))} />
            </CardContent>
          </Card>
        )}
        {questions.length === 0 && <Empty>No questions.</Empty>}
        {questions.map((q: any) => (
          <Card key={q.id}>
            <CardHeader>
              <CardTitle>{q.subject}</CardTitle>
              <CardDescription>
                {q.asker_name} · {fmtTime(q.created_at)}
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-3 text-sm">
              <p className="break-words whitespace-pre-wrap">{q.content}</p>
              {q.reply && (
                <div className="border-l-2 pl-3">
                  <div className="text-muted-foreground text-xs">Reply · {fmtTime(q.replied_at)}</div>
                  <p className="break-words whitespace-pre-wrap">{q.reply}</p>
                </div>
              )}
              {!q.reply && !is_admin && <p className="text-muted-foreground text-xs">Waiting for a reply.</p>}
              {is_admin && (
                <form
                  className="grid gap-2"
                  onSubmit={async (e) => {
                    e.preventDefault();
                    const reply = new FormData(e.currentTarget).get("reply");
                    done(await act(() => api(`${base}/manage/questions/${q.id}/reply`, { body: { reply } }), "Reply sent"));
                  }}
                >
                  <Textarea name="reply" placeholder="Write a reply" defaultValue={q.reply ?? ""} maxLength={4000} required />
                  <div>
                    <Button type="submit" size="sm">
                      {q.reply ? "Update reply" : "Reply"}
                    </Button>
                  </div>
                </form>
              )}
            </CardContent>
          </Card>
        ))}
      </section>
    </div>
  );
}
