import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createConfirm } from "@ui-sdk/react/alert-dialog";
import { createToaster } from "@ui-sdk/react/toast";
import { getServices, post, type Tone } from "./api";

export const toast = createToaster();
export const confirm = createConfirm<{
  title: string;
  message: string;
  label: string;
  agent: string;
}>();

// Whole class names, so Tailwind can see them.
export const dotColor: Record<Tone, string> = {
  good: "bg-good",
  warn: "bg-warn",
  bad: "bg-bad",
  idle: "bg-label-2",
};
export const textColor: Record<Tone, string> = {
  good: "text-good",
  warn: "text-warn",
  bad: "text-bad",
  idle: "text-label-2",
};

export const buttonClass =
  "min-h-[50px] w-full rounded-xl text-[17px] font-semibold lg:min-h-10 lg:w-auto lg:px-4 lg:text-[15px]";
export const sectionClass =
  "mx-1 mt-6 mb-2 text-[13px] font-semibold uppercase tracking-wider text-label-2";

export function Icon({ children, size = 24 }: { children: ReactNode; size?: number }) {
  return (
    <svg
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      {children}
    </svg>
  );
}

/** The service list, refreshed every 3 seconds, and not at all while the page is hidden. */
export function useServices() {
  return useQuery({
    queryKey: ["services"],
    queryFn: getServices,
    refetchInterval: 3000,
    refetchIntervalInBackground: false,
  });
}

/** Presses a button on a service, reports the result in a toast, then refreshes the list. */
export function useAct() {
  const client = useQueryClient();
  const mutation = useMutation({
    mutationFn: ({ path }: { path: string; doing: string; done: string }) => post(path),
    onSuccess: (result, { doing, done }) => {
      const last = result.output?.split("\n").filter(Boolean).at(-1);
      if (result.ok) toast(done, { description: last });
      else toast(`${doing} didn't work`, { description: result.error ?? last });
    },
    onError: (_e, { doing }) =>
      toast(`${doing} didn't work`, { description: "The hub didn't answer." }),
    onSettled: async () => {
      await new Promise((r) => setTimeout(r, 400));
      await client.invalidateQueries({ queryKey: ["services"] });
    },
  });
  return mutation.mutateAsync;
}

export type Route = { kind: "home" } | { kind: "logs" } | { kind: "service"; id: string };

function readRoute(): Route {
  const [, a, b] = location.hash.split("/");
  if (a === "s" && b) return { kind: "service", id: b };
  if (a === "logs") return { kind: "logs" };
  return { kind: "home" };
}

export function useRoute(): [Route, (hash: string) => void] {
  const [route, setRoute] = useState(readRoute);
  useEffect(() => {
    const on = () => setRoute(readRoute());
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return [route, (hash) => (location.hash = hash)];
}

/** True on a PC-sized window, where the page shows the sidebar and the detail side by side. */
export function useDesktop(): boolean {
  const query = "(min-width: 1024px)";
  const [on, setOn] = useState(() => window.matchMedia(query).matches);
  useEffect(() => {
    const m = window.matchMedia(query);
    const change = () => setOn(m.matches);
    m.addEventListener("change", change);
    return () => m.removeEventListener("change", change);
  }, []);
  return on;
}

/** Starred services, kept in this browser. Storage can be blocked, so every access is guarded. */
export function useStarred(): [Set<string>, (id: string) => void] {
  const key = "devhub.starred";
  const [set, setSet] = useState<Set<string>>(() => {
    try {
      return new Set<string>(JSON.parse(localStorage.getItem(key) ?? "[]"));
    } catch {
      return new Set();
    }
  });
  const toggle = (id: string) =>
    setSet((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      try {
        localStorage.setItem(key, JSON.stringify([...next]));
      } catch {
        /* the star still works for this visit */
      }
      return next;
    });
  return [set, toggle];
}
