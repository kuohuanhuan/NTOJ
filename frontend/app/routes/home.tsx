import { Pin } from "lucide-react";

import type { Route } from "./+types/home";
import { Empty, PageHeader } from "~/components/common";
import { Markdown } from "~/components/markdown";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { load } from "~/lib/api";
import { fmtTime } from "~/lib/format";
import { cn } from "~/lib/utils";

export interface Bulletin {
  id: number;
  title: string;
  content: string;
  color: string;
  pinned: boolean;
  created_at: string;
  author_name: string | null;
}

// Fixed palette: the server only ever stores one of these names.
export const BULLETIN_COLORS: Record<string, string> = {
  red: "border-l-red-500",
  orange: "border-l-orange-500",
  yellow: "border-l-yellow-500",
  green: "border-l-green-500",
  blue: "border-l-blue-500",
  purple: "border-l-purple-500",
};

export async function clientLoader({ request }: Route.ClientLoaderArgs) {
  return load<Bulletin[]>("/bulletins", request);
}

export default function Home({ loaderData }: Route.ComponentProps) {
  return (
    <>
      <PageHeader title="Bulletin" />
      {loaderData.length === 0 && <Empty>No announcements yet.</Empty>}
      <div className="grid gap-4">
        {loaderData.map((b) => (
          <Card key={b.id} className={cn(b.color && "border-l-4", BULLETIN_COLORS[b.color])}>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                {b.pinned && <Pin className="size-4 shrink-0" aria-label="Pinned" />}
                {b.title}
              </CardTitle>
              <CardDescription>
                {b.author_name ?? "Administrator"} · {fmtTime(b.created_at)}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <Markdown>{b.content}</Markdown>
            </CardContent>
          </Card>
        ))}
      </div>
    </>
  );
}
