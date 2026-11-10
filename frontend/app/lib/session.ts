import { useRouteLoaderData } from "react-router";

export interface Language {
  id: string;
  name: string;
}

export interface Me {
  id: number;
  name: string;
  email: string;
  role: "admin" | "user";
  photo_url: string;
  cover_url: string;
  motto: string;
  last_language: string;
  unread_replies: number;
}

export interface Session {
  site: { title: string; allow_signup: boolean; languages: Language[] };
  account: Me | null;
  /** Contest the account is confined to while it runs, if any. */
  lockdown: number | null;
}

export function useSession(): Session {
  return useRouteLoaderData("root") as Session;
}

export function useLanguageName() {
  const { site } = useSession();
  return (id: string) => site.languages.find((l) => l.id === id)?.name ?? id;
}
