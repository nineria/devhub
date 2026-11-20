import type { ReactNode } from "react";
import { Button } from "@ui-sdk/react/button";
import { Input } from "@ui-sdk/react/input";
import {
  SegmentedControl,
  SegmentedControlIndicator,
  SegmentedControlItem,
} from "@ui-sdk/react/segmented-control";
import { Menu, MenuContent, MenuItem, MenuTrigger } from "@ui-sdk/react/menu";
import { Toggle, ToggleIcon } from "@ui-sdk/react/toggle";
import { ago, lampTone, needsAttention, seconds, statusOf, type Service } from "./api";
import { LogView } from "./Logs";
import { useServiceActions } from "./ServiceActions";
import { Lamp, buttonClass, copyText, pill, textColor, toast, useEditor, useMeta } from "./shared";

/** Groups come from the config, in the order they first appear. */
const groupLabel = (g: string) => (g ? g.charAt(0).toUpperCase() + g.slice(1) : "Other");

export type Only = "all" | "attention";

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/** Hosts Ostler can be reached on (domain, localhost, LAN, VPN), the current one first. */
function useHosts(): string[] {
  const urls = useMeta().data?.urls ?? [];
  const hosts = [location.hostname];
  for (const u of urls) {
    try {
      const h = new URL(u).hostname;
      if (!hosts.includes(h)) hosts.push(h);
    } catch {
      /* ignore a malformed address */
    }
  }
  return hosts;
}

/** Every address a link opens at: ":8080" is tried on each host, a full URL or "/path" stays as written. */
export function linkUrls(path: string, hosts: string[]): string[] {
  if (path.startsWith(":"))
    return hosts.map((h) => `http://${h.includes(":") ? `[${h}]` : h}${path}`);
  return [path];
}

/** All of a service's addresses, each with a Copy button, to type or send to another device. */
function Addresses({ s }: { s: Service }) {
  const hosts = useHosts();
  if (!s.links.length) return null;
  return (
    <>
      {s.links.map((l) => (
        <section
          key={`${l.label}${l.path}`}
          aria-label={l.label}
          className="mb-3 rounded-(--radius-card) border border-line bg-card px-4 py-3 shadow-xs lg:mb-3 lg:py-2.5"
        >
          <p className="m-0 mb-1 text-[15px] font-medium text-label-2 lg:text-[13px]">{l.label}</p>
          <ul className="m-0 list-none p-0">
            {linkUrls(l.path, hosts).map((u) => (
              <li key={u} className="flex items-center justify-between gap-2 py-0.5">
                <a
                  href={u}
                  target="_blank"
                  rel="noreferrer"
                  className="num min-w-0 truncate text-[15px] text-inherit lg:text-[13px]"
                >
                  {u}
                </a>
                <Button
                  onClick={() =>
                    toast(copyText(u) ? "Copied address" : "Couldn't copy", { description: u })
                  }
                >
                  Copy<span className="sr-only"> {u}</span>
                </Button>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </>
  );
}

export function Health({ services, compact }: { services: Service[]; compact?: boolean }) {
  const attention = services.filter(needsAttention);
  const running = services.filter((s) => statusOf(s).tone === "good").length;
  const stopped = services.filter((s) => s.disabled || (s.kind === "process" && !s.want)).length;
  const rest = [`${running} running`, stopped > 0 ? `${stopped} stopped` : ""]
    .filter(Boolean)
    .join(", ");
  return (
    <header className={compact ? "px-2 pt-1 pb-3" : "pt-6 pb-4"}>
      <h1 className={`title ${compact ? "text-[18px]" : "text-[34px]"}`}>Services</h1>
      <p className={`mt-1 text-label-2 ${compact ? "text-[13px]" : "text-[15px]"}`}>
        {attention.length > 0 && (
          <>
            <span className="font-semibold text-warn">
              {attention.length} need{attention.length === 1 ? "s" : ""} attention
            </span>
            {", "}
          </>
        )}
        {rest}.
      </p>
    </header>
  );
}

export function Filters({
  query,
  setQuery,
  only,
  setOnly,
}: {
  query: string;
  setQuery: (q: string) => void;
  only: Only;
  setOnly: (o: Only) => void;
}) {
  return (
    <div className="mb-3 grid gap-2">
      <Input
        id="service-search"
        aria-label="Search services"
        placeholder="Search services"
        value={query}
        onValueChange={(v) => setQuery(v)}
      />
      <SegmentedControl aria-label="Show" value={only} onValueChange={(v) => setOnly(v as Only)}>
        <SegmentedControlIndicator />
        <SegmentedControlItem value="all">All</SegmentedControlItem>
        <SegmentedControlItem value="attention">Needs attention</SegmentedControlItem>
      </SegmentedControl>
    </div>
  );
}

function Counts({ s }: { s: Service }) {
  return (
    <>
      {s.errors > 0 && (
        <span className="inline-flex items-center gap-1.5 font-semibold">
          <Lamp tone="bad" small /> {plural(s.errors, "error")}
        </span>
      )}
      {s.warnings > 0 && (
        <span className="inline-flex items-center gap-1.5 font-semibold">
          <Lamp tone="warn" small /> {plural(s.warnings, "warning")}
        </span>
      )}
    </>
  );
}

function Row({
  s,
  selected,
  starred,
  toggleStar,
}: {
  s: Service;
  selected: boolean;
  starred: boolean;
  toggleStar: (id: string) => void;
}) {
  const st = statusOf(s);
  const quiet = st.tone === "good" || st.tone === "idle";
  return (
    <li
      data-service={s.id}
      className={`flex items-center border-t-[0.5px] border-line first:border-t-0 ${selected ? "bg-accent/10" : "hover:bg-muted"}`}
    >
      <a
        className={`flex min-h-16 min-w-0 flex-1 items-center gap-3 py-2.5 pr-1 pl-3 text-inherit no-underline lg:min-h-12 lg:py-1.5`}
        href={`#/s/${s.id}`}
        aria-current={selected ? "page" : undefined}
      >
        <Lamp tone={lampTone(s)} busy={!!s.busy} />
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-[17px] font-semibold lg:text-[14px]">{s.name}</span>
          <span className="flex flex-wrap items-center gap-x-3 text-[15px] text-label-2 lg:text-[12px]">
            <span className={quiet ? "" : `font-semibold ${textColor[st.tone]}`}>{st.label}</span>
            <Counts s={s} />
          </span>
        </span>
        {s.kind !== "task" && !s.disabled && (
          <span className={`num text-[13px] lg:text-[12px] text-label-2`}>{ago(s.since)}</span>
        )}
        <svg
          className="flex-none opacity-60 lg:hidden"
          viewBox="0 0 24 24"
          width="18"
          height="18"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="m9 5 7 7-7 7" />
        </svg>
      </a>
      <Toggle
        className="mr-1 size-11 flex-none border-0 bg-transparent text-inherit opacity-60 data-pressed:text-warn data-pressed:opacity-100 lg:size-9"
        pressed={starred}
        onPressedChange={() => toggleStar(s.id)}
        aria-label={`Star ${s.name}`}
      >
        <ToggleIcon data-variant="fill">
          <svg
            viewBox="0 0 24 24"
            width="18"
            height="18"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.8"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="m12 3 2.7 5.6 6.1.8-4.5 4.3 1.1 6.1L12 16.9 6.6 19.8l1.1-6.1L3.2 9.4l6.1-.8z" />
          </svg>
        </ToggleIcon>
      </Toggle>
    </li>
  );
}

export function ServiceList({
  services,
  query,
  only,
  selectedId,
  starred,
  toggleStar,
}: {
  services: Service[];
  query: string;
  only: Only;
  selectedId: string | null;
  starred: Set<string>;
  toggleStar: (id: string) => void;
}) {
  const q = query.trim().toLowerCase();
  const keep = (s: Service) => {
    if (q && !s.name.toLowerCase().includes(q)) return false;
    return only === "attention" ? needsAttention(s) : true;
  };
  const visible = services.filter(keep);
  const canEdit = useMeta().data?.canEdit === true;
  const openEditor = useEditor();
  const sections: { id: string; label: string; items: Service[] }[] = [
    { id: "starred", label: "Starred", items: visible.filter((s) => starred.has(s.id)) },
    ...[...new Set(services.map((s) => s.group))].map((g) => ({
      id: g,
      label: groupLabel(g),
      items: visible.filter((s) => s.group === g && !starred.has(s.id)),
    })),
  ].filter((section) => section.items.length > 0);

  if (sections.length === 0) {
    return (
      <p className="mt-2.5 text-[15px] lg:mt-2 lg:text-[13px] text-label-2">
        {only === "attention" ? "Nothing needs attention." : "No service matches."}
      </p>
    );
  }
  return (
    <>
      {sections.map((section) => (
        <section key={section.id} aria-label={section.label}>
          <h2 className="m-0 flex items-baseline justify-between px-3 pt-6 pb-2 text-[13px] font-medium text-label-2 lg:pt-5 lg:pb-1.5 lg:text-[12px]">
            {section.label}
            <span className="flex items-center gap-1">
              <span className="num font-normal">{section.items.length}</span>
              {canEdit && section.id !== "starred" && section.id !== "" && (
                <button
                  type="button"
                  className="-my-2 -mr-2 inline-flex size-8 cursor-pointer items-center justify-center rounded-full border-0 bg-transparent p-0 text-label-2 hover:bg-muted hover:text-accent"
                  aria-label={`Add service to ${section.label}`}
                  title={`Add service to ${section.label}`}
                  onClick={() => openEditor(undefined, section.id)}
                >
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 16 16"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.75"
                    strokeLinecap="round"
                    aria-hidden="true"
                  >
                    <path d="M8 3v10M3 8h10" />
                  </svg>
                </button>
              )}
            </span>
          </h2>
          <ul className="m-0 list-none overflow-hidden rounded-(--radius-card) bg-card p-0 shadow-xs lg:rounded-none lg:bg-transparent lg:shadow-none">
            {section.items.map((s) => (
              <Row
                key={s.id}
                s={s}
                selected={s.id === selectedId}
                starred={starred.has(s.id)}
                toggleStar={toggleStar}
              />
            ))}
          </ul>
        </section>
      ))}
    </>
  );
}

function Actions({ s }: { s: Service }) {
  const openEditor = useEditor();
  const canEdit = useMeta().data?.canEdit === true;
  const { act, ask, path, start, stop, disable, enable, restart, remove, stopped, live } =
    useServiceActions(s);
  const name = <span className="sr-only"> {s.name}</span>;

  // Everything that is rarely needed, or can't be undone, lives in the menu, away from Restart.
  const menu: { value: string; label: string; bad?: boolean; run: () => void }[] = [];
  if (canEdit) menu.push({ value: "edit", label: "Edit", run: () => openEditor(s.id) });
  if (live) menu.push({ value: "disable", label: "Disable", bad: true, run: () => void disable() });
  if (canEdit) menu.push({ value: "delete", label: "Delete", bad: true, run: () => void remove() });

  const secondary: ReactNode[] = [];
  if (s.kind === "process" && live && !stopped) {
    secondary.push(
      <Button key="stop" className={buttonClass} onClick={() => void stop()}>
        Stop{name}
      </Button>,
    );
  }
  if (live) {
    for (const b of s.buttons) {
      secondary.push(
        <Button
          key={b.id}
          className={buttonClass}
          onClick={async () => {
            if (
              b.confirm &&
              !(await ask(`${b.label}?`, b.confirm, b.label, `${b.label} on ${s.name}`))
            )
              return;
            await act({
              path: path(`buttons/${b.id}`),
              doing: b.label,
              done: `${b.label}: done`,
            });
          }}
        >
          {b.label}
          <span className="sr-only"> for {s.name}</span>
        </Button>,
      );
    }
    for (const l of s.links) {
      secondary.push(
        <a
          key={l.path}
          className={`${buttonClass} btn text-accent no-underline focus-visible:outline-2 focus-visible:outline-accent`}
          href={l.path.startsWith(":") ? `http://${location.hostname}${l.path}` : l.path}
          target="_blank"
          rel="noreferrer"
        >
          {l.label}
        </a>,
      );
    }
  }
  if (s.disabled) {
    secondary.push(
      <Button key="enable" className={buttonClass} onClick={() => void enable()}>
        Enable{name}
      </Button>,
    );
  }

  return (
    <div className="flex flex-col gap-3" role="group" aria-label={`${s.name} actions`}>
      <div className="flex flex-col gap-2.5 lg:flex-row lg:flex-nowrap lg:justify-end lg:gap-2">
        {s.kind === "process" && live && (
          <Button
            className={buttonClass}
            data-variant="solid"
            onClick={async () => {
              if (stopped) {
                await start();
                return;
              }
              // No question: the build runs first, and a failed build leaves the old version up.
              await restart();
            }}
          >
            {stopped ? "Start" : "Rebuild and restart"}
            {name}
          </Button>
        )}
        {s.blocker && live && (
          <Button
            className={buttonClass}
            data-variant="solid"
            onClick={async () => {
              const b = s.blocker!;
              if (
                !(await ask(
                  `Free port ${b.port}?`,
                  `This stops pid ${b.pid} (${b.cmd}), which is using the port, and then starts ${s.name}.`,
                  "Stop it and start",
                  `Stops pid ${b.pid} on port ${b.port}`,
                ))
              )
                return;
              await act({
                path: path("free-port"),
                doing: `Freeing port ${b.port}`,
                done: `Port ${b.port} freed`,
              });
            }}
          >
            Free port {s.blocker.port} and start
          </Button>
        )}
        {secondary.length > 0 && <div className="grid gap-2.5 lg:contents">{secondary}</div>}
        {menu.length > 0 && (
          <Menu onAction={(v) => menu.find((m) => m.value === v)?.run()}>
            <MenuTrigger className={`${buttonClass} btn`}>
              More<span className="sr-only"> actions for {s.name}</span>
            </MenuTrigger>
            <MenuContent>
              {menu.map((m) => (
                <MenuItem key={m.value} value={m.value} className={m.bad ? "text-bad" : undefined}>
                  {m.label}
                </MenuItem>
              ))}
            </MenuContent>
          </Menu>
        )}
      </div>
      {s.busy && (
        <p className="m-0 text-[15px] text-label-2 lg:text-[13px]" role="status">
          Running “{s.busy}”…
        </p>
      )}
    </div>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-[13px] text-label-2 lg:text-[12px]">{label}</dt>
      <dd className="m-0 mt-0.5 text-[17px] font-medium lg:text-[14px]">{children}</dd>
    </div>
  );
}

export function Detail({ s, desktop }: { s: Service; services?: Service[]; desktop: boolean }) {
  const st = statusOf(s);
  return (
    <div data-service={s.id} className={desktop ? "flex min-h-0 flex-1 flex-col" : ""}>
      {!desktop && (
        <a
          className="-ml-1.5 inline-flex min-h-11 items-center gap-0.5 px-1.5 text-[17px] text-accent no-underline"
          href="#/"
        >
          <svg
            viewBox="0 0 24 24"
            width="20"
            height="20"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="m15 5-7 7 7 7" />
          </svg>
          Services
        </a>
      )}
      <header className="mb-4 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0 lg:max-w-[60ch]">
          <h1 className="title m-0 flex flex-wrap items-center gap-x-3 gap-y-2 text-[34px] lg:text-[22px]">
            {s.name}
            <span
              className={`inline-flex items-center gap-2 rounded-full px-3 py-1 text-[15px] font-medium tracking-normal lg:px-2.5 lg:py-0.5 lg:text-[12px] ${pill[lampTone(s)]}`}
            >
              <Lamp tone={lampTone(s)} busy={!!s.busy} small />
              {st.label}
            </span>
          </h1>
          <p className="mt-2 mb-0 text-[15px] text-label-2 lg:text-[13px]">{s.desc}</p>
        </div>
        <Actions s={s} />
      </header>
      <Addresses s={s} />
      <section
        aria-label="Status"
        className="mb-6 rounded-(--radius-card) border border-line bg-card px-4 py-3.5 shadow-xs lg:mb-5 lg:py-3"
      >
        <dl className="m-0 grid grid-cols-2 gap-x-6 gap-y-3 lg:grid-cols-[repeat(auto-fit,minmax(8.5rem,1fr))]">
          {s.kind !== "task" && !s.disabled && <Fact label="Up for">{ago(s.since)}</Fact>}
          <Fact label="Errors">
            <span className={s.errors > 0 ? "text-bad" : ""}>{s.errors}</span>
          </Fact>
          <Fact label="Warnings">
            <span className={s.warnings > 0 ? "text-warn" : ""}>{s.warnings}</span>
          </Fact>
          {s.build && (
            <Fact label="Last build">
              <span className={s.build.ok ? "" : "text-bad"}>
                {s.build.ok ? "Passed" : "Failed"}, {seconds(s.build.ms)}
              </span>
            </Fact>
          )}
          {s.kind === "process" && <Fact label="Restarts">{s.restarts}</Fact>}
          {s.kind === "task" && s.detail && <Fact label="Detail">{s.detail}</Fact>}
        </dl>
        {s.note && (
          <p className="mt-3 mb-0 text-[15px] font-medium text-warn lg:text-[13px]">{s.note}</p>
        )}
      </section>
      <LogView
        url={`/api/services/${s.id}/logs`}
        clearPath={`services/${s.id}/clear`}
        fill={desktop}
      />
    </div>
  );
}

export function AllLogs({ services, fill }: { services: Service[]; fill?: boolean }) {
  const names = Object.fromEntries(services.map((s) => [s.id, s.name]));
  return (
    <div className={fill ? "flex min-h-0 flex-1 flex-col" : ""}>
      <LogView
        url="/api/logs"
        clearPath="logs/clear"
        names={names}
        fill={fill}
        title={fill ? "All logs" : "All services"}
      />
    </div>
  );
}
