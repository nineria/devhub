import { useEffect, useState } from "react";
import {
  AlertDialog,
  AlertDialogBackdrop,
  AlertDialogClose,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogTitle,
} from "@ui-sdk/react/alert-dialog";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@ui-sdk/react/tabs";
import { Toast, ToastDescription, ToastRegion, ToastTitle } from "@ui-sdk/react/toast";
import { AllLogs, Detail, Filters, Health, ServiceList, type Only } from "./Services";
import { Icon, confirm, toast, useDesktop, useRoute, useServices, useStarred } from "./shared";

const TABS = [
  {
    id: "services",
    label: "Services",
    icon: <path d="M4 5h6v6H4zM14 5h6v6h-6zM4 15h6v6H4zM14 15h6v6h-6z" />,
  },
  { id: "logs", label: "Logs", icon: <path d="M5 6h14M5 12h14M5 18h9" /> },
] as const;

function Dialogs() {
  return (
    <>
      <AlertDialog controller={confirm.controller}>
        <AlertDialogBackdrop />
        <AlertDialogContent<{ title: string; message: string; label: string; agent: string }>>
          {(state) => (
            <>
              <div data-slot="stack">
                <AlertDialogTitle>{state.payload?.title}</AlertDialogTitle>
                <AlertDialogDescription>{state.payload?.message}</AlertDialogDescription>
              </div>
              <div data-slot="row">
                <AlertDialogClose>Cancel</AlertDialogClose>
                <AlertDialogClose
                  data-variant="solid"
                  value="confirm"
                  data-agent-confirm={state.payload?.agent}
                >
                  {state.payload?.label}
                </AlertDialogClose>
              </div>
            </>
          )}
        </AlertDialogContent>
      </AlertDialog>

      <ToastRegion toaster={toast} data-agent-hidden>
        {() => (
          <Toast>
            <div data-slot="stack">
              <ToastTitle />
              <ToastDescription />
            </div>
          </Toast>
        )}
      </ToastRegion>
    </>
  );
}

/** "/" jumps to the search box on a PC, as in Tilt. */
function useSlashShortcut() {
  useEffect(() => {
    const on = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      if (
        e.key !== "/" ||
        e.metaKey ||
        e.ctrlKey ||
        el?.closest("input, textarea, [contenteditable]")
      )
        return;
      const box = document.getElementById("service-search");
      if (box) {
        e.preventDefault();
        box.focus();
      }
    };
    window.addEventListener("keydown", on);
    return () => window.removeEventListener("keydown", on);
  }, []);
}

export function App() {
  const { data: services, isError } = useServices();
  const [route, go] = useRoute();
  const desktop = useDesktop();
  const [starred, toggleStar] = useStarred();
  const [query, setQuery] = useState("");
  const [only, setOnly] = useState<Only>("all");
  useSlashShortcut();

  const selected = route.kind === "service" ? services?.find((s) => s.id === route.id) : undefined;
  const selectedId = selected?.id ?? null;

  if (services === undefined) {
    return (
      <main className="mx-auto max-w-160 px-4 py-10">
        <p className="text-[17px] text-label-2" role="status">
          {isError ? "Can't reach the hub." : "Loading…"}
        </p>
      </main>
    );
  }

  const list = (
    <>
      <Filters query={query} setQuery={setQuery} only={only} setOnly={setOnly} />
      <ServiceList
        services={services}
        query={query}
        only={only}
        selectedId={selectedId}
        starred={starred}
        toggleStar={toggleStar}
      />
    </>
  );
  const stale = isError && (
    <p className="mb-2.5 text-[15px] font-semibold text-warn" role="status">
      Update failed. Showing the last result.
    </p>
  );

  // PC: a sidebar with the list, and the selected service (or the combined log) beside it.
  if (desktop) {
    const logsActive = route.kind !== "service";
    const total = services.reduce((n, s) => n + s.errors, 0);
    return (
      <div className="grid h-dvh grid-cols-[22rem_minmax(0,1fr)]">
        <aside
          className="flex min-h-0 flex-col overflow-y-auto border-r-[0.5px] border-line px-4 py-4"
          aria-label="Services"
        >
          <Health services={services} compact />
          {stale}
          <a
            href="#/logs"
            aria-current={logsActive ? "page" : undefined}
            className={`mb-3 flex min-h-11 items-center justify-between rounded-xl px-3.5 text-[16px] font-semibold text-inherit no-underline focus-visible:outline-2 focus-visible:outline-accent ${logsActive ? "bg-card" : "hover:bg-muted"}`}
          >
            All logs
            {total > 0 && (
              <span className="text-[14px] text-bad">
                {total} error{total === 1 ? "" : "s"}
              </span>
            )}
          </a>
          {list}
        </aside>
        <main className="flex min-h-0 flex-col overflow-y-auto px-6 py-5">
          {selected ? (
            <Detail s={selected} services={services} desktop />
          ) : (
            <AllLogs services={services} fill />
          )}
        </main>
        <Dialogs />
      </div>
    );
  }

  // Phone: one column and a tab bar at the bottom. The bar is for moving between sections only.
  const tab = route.kind === "logs" ? "logs" : "services";
  return (
    <div>
      <main className="mx-auto max-w-160 px-4 pt-[max(1rem,env(safe-area-inset-top))] pb-[calc(var(--tabbar)+env(safe-area-inset-bottom)+24px)]">
        <Tabs value={tab} onValueChange={(v) => go(v === "logs" ? "#/logs" : "#/")}>
          {tab === "services" && !selected && <Health services={services} />}
          {stale}
          <TabsPanel value="services">
            {selected ? <Detail s={selected} services={services} desktop={false} /> : list}
          </TabsPanel>
          <TabsPanel value="logs">
            <h1 className="px-1 pt-6 pb-1 text-[34px] leading-[1.12] font-bold tracking-tight">
              Logs
            </h1>
            <AllLogs services={services} />
          </TabsPanel>
          <TabsList
            aria-label="Sections"
            className="fixed inset-x-0 bottom-0 z-10 grid h-[calc(var(--tabbar)+env(safe-area-inset-bottom))] grid-cols-2 gap-0 rounded-none border-t-[0.5px] border-line bg-[light-dark(rgb(249_249_249/0.94),rgb(29_29_31/0.94))] pb-[env(safe-area-inset-bottom)] backdrop-blur-xl"
          >
            {TABS.map((t) => (
              <TabsTab
                key={t.id}
                value={t.id}
                className="flex h-(--tabbar) min-h-11 flex-col items-center justify-center gap-0.5 rounded-none border-0 bg-transparent p-0 text-[11px] font-semibold text-label-2 data-selected:text-accent"
              >
                <Icon>{t.icon}</Icon>
                <span>{t.label}</span>
              </TabsTab>
            ))}
          </TabsList>
        </Tabs>
      </main>
      <Dialogs />
    </div>
  );
}
