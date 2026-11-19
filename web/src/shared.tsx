import { useEffect, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createConfirm } from "@ui-sdk/react/alert-dialog";
import { createToaster } from "@ui-sdk/react/toast";
import { createContext, useContext } from "react";
import { getMeta, getServices, post, type Tone } from "./api";

export const toast = createToaster();
export const confirm = createConfirm<{
  title: string;
  message: string;
  label: string;
  agent: string;
}>();

// Whole class names, so Tailwind can see them.
export const textColor: Record<Tone, string> = {
  good: "text-good",
  warn: "text-warn",
  bad: "text-bad",
  idle: "text-label-2",
};

export const pill: Record<Tone, string> = {
  good: "bg-good/12 text-good",
  warn: "bg-warn/12 text-warn",
  bad: "bg-bad/12 text-bad",
  idle: "bg-label-2/12 text-label-2",
};

/** A status lamp. Its shape (circle, triangle, square, ring) says the state even without color. */
export function Lamp({ tone, busy, small }: { tone: Tone; busy?: boolean; small?: boolean }) {
  return (
    <span
      className={`lamp lamp-${tone} ${busy ? "lamp-busy" : ""} ${small ? "lamp-sm" : ""}`}
      aria-hidden="true"
    />
  );
}

export const buttonClass = "w-full lg:w-auto lg:flex-none lg:px-3.5";
export const sectionClass = "m-0 text-[15px] font-semibold lg:text-[13px]";

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

/** Whether Ostler lets this page add, change and delete services. */
export function useMeta() {
  return useQuery({ queryKey: ["meta"], queryFn: getMeta, staleTime: Infinity });
}

/** Opens the service editor: for a new service (optionally in a group), or for the one with this id. */
export const EditorContext = createContext<(id?: string, group?: string) => void>(() => {});
export const useEditor = () => useContext(EditorContext);

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
      toast(`${doing} didn't work`, { description: "Ostler didn't answer." }),
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
  const key = "ostler.starred";
  const [set, setSet] = useState<Set<string>>(() => {
    try {
      // Stars saved before the rename are still read, then saved under the new key on the next toggle.
      return new Set<string>(
        JSON.parse(localStorage.getItem(key) ?? localStorage.getItem("devhub.starred") ?? "[]"),
      );
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
