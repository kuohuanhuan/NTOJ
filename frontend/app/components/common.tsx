import { useState } from "react";
import { Link, useSearchParams } from "react-router";
import { ChevronLeft, ChevronRight } from "lucide-react";

import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "~/components/ui/dialog";
import { Label } from "~/components/ui/label";
import { STATES, type Tone } from "~/lib/format";
import { cn } from "~/lib/utils";

const TONES: Record<Tone, string> = {
  ok: "text-emerald-600 dark:text-emerald-400",
  partial: "text-sky-600 dark:text-sky-400",
  bad: "text-red-600 dark:text-red-400",
  warn: "text-amber-600 dark:text-amber-400",
  muted: "text-muted-foreground",
};

export function StateText({ state, long, className }: { state: number | null | undefined; long?: boolean; className?: string }) {
  if (state == null) return <span className="text-muted-foreground">—</span>;
  const info = STATES[state] ?? { short: String(state), long: String(state), tone: "muted" as Tone };
  return (
    <span className={cn("font-medium", TONES[info.tone], className)} title={info.long}>
      {long ? info.long : info.short}
    </span>
  );
}

export function PageHeader({ title, description, children }: { title: React.ReactNode; description?: React.ReactNode; children?: React.ReactNode }) {
  return (
    <div className="mb-5 flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        <h1 className="truncate text-2xl font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-muted-foreground mt-1 text-sm">{description}</p>}
      </div>
      {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
    </div>
  );
}

export function Field({ label, hint, children, className }: { label: string; hint?: string; children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("grid content-start gap-1.5", className)}>
      <Label>{label}</Label>
      {children}
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </div>
  );
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <p className="text-muted-foreground py-10 text-center text-sm">{children}</p>;
}

export function UserLink({ id, name, className }: { id: number; name: string; className?: string }) {
  return (
    <Link to={`/users/${id}`} className={cn("hover:underline", className)}>
      {name}
    </Link>
  );
}

/** Pager driven by the `page` search parameter. */
export function Pagination({ total, page, perPage }: { total: number; page: number; perPage: number }) {
  const [params] = useSearchParams();
  const pages = Math.max(1, Math.ceil(total / perPage));
  if (pages <= 1) return null;
  const href = (p: number) => {
    const next = new URLSearchParams(params);
    next.set("page", String(p));
    return "?" + next.toString();
  };
  return (
    <div className="mt-4 flex items-center justify-center gap-3 text-sm">
      <Button asChild={page > 1} variant="outline" size="sm" disabled={page <= 1}>
        {page > 1 ? (
          <Link to={href(page - 1)}>
            <ChevronLeft /> Previous
          </Link>
        ) : (
          <span>
            <ChevronLeft /> Previous
          </span>
        )}
      </Button>
      <span className="text-muted-foreground tabular-nums">
        {page} / {pages}
      </span>
      <Button asChild={page < pages} variant="outline" size="sm" disabled={page >= pages}>
        {page < pages ? (
          <Link to={href(page + 1)}>
            Next <ChevronRight />
          </Link>
        ) : (
          <span>
            Next <ChevronRight />
          </span>
        )}
      </Button>
    </div>
  );
}

/** A button that asks for confirmation before running a destructive action. */
export function ConfirmButton({
  title,
  description,
  onConfirm,
  children,
  confirmLabel = "Confirm",
  ...props
}: React.ComponentProps<typeof Button> & {
  title: string;
  description?: string;
  confirmLabel?: string;
  onConfirm: () => void | Promise<unknown>;
}) {
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button variant="destructive" size="sm" {...props}>
          {children}
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description ?? "This cannot be undone."}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="outline">Cancel</Button>
          </DialogClose>
          <Button
            variant="destructive"
            disabled={busy}
            onClick={async () => {
              setBusy(true);
              try {
                await onConfirm();
                setOpen(false);
              } finally {
                setBusy(false);
              }
            }}
          >
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
