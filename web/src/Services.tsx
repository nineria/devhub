import { Button } from "@ui-sdk/react/button";
import { Input } from "@ui-sdk/react/input";
import {
  SegmentedControl,
  SegmentedControlIndicator,
  SegmentedControlItem,
} from "@ui-sdk/react/segmented-control";
import { Toggle, ToggleIcon } from "@ui-sdk/react/toggle";
import { ago, seconds, statusOf, type Service } from "./api";
import { LogView } from "./Logs";
import { buttonClass, confirm, dotColor, sectionClass, textColor, useAct } from "./shared";

/** Groups come from the config, in the order they first appear. */
const groupLabel = (g: string) => g.charAt(0).toUpperCase() + g.slice(1);

export type Only = "all" | "attention";

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

export function Health({ services, compact }: { services: Service[]; compact?: boolean }) {
  const tones = services.map((s) => statusOf(s).tone);
  const attention = services.filter((_, i) => tones[i] === "warn" || tones[i] === "bad");
  const running = tones.filter((t) => t === "good").length;
  const stopped = services.filter((s) => s.disabled || (s.kind === "process" && !s.want)).length;
  const summary = [`${running} running`, stopped > 0 ? `${stopped} stopped` : ""]
    .filter(Boolean)
    .join(", ");
  const ok = attention.length === 0;
  return (
    <header className={compact ? "px-1 pt-1 pb-3" : "px-1 pt-6 pb-5"}>
      <h1
        className={`${compact ? "text-[24px]" : "text-[34px]"} leading-[1.12] font-bold tracking-tight ${ok ? "text-good" : "text-bad"}`}
      >
        {ok ? "All good" : `${attention.length} need${attention.length === 1 ? "s" : ""} attention`}
      </h1>
      <p className="mt-1 text-[15px] text-label-2">
        {ok ? `${summary}.` : `${attention.map((s) => s.name).join(", ")}. ${summary}.`}
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
        <span className="font-semibold text-bad"> · {plural(s.errors, "error")}</span>
      )}
      {s.warnings > 0 && (
        <span className="font-semibold text-warn"> · {plural(s.warnings, "warning")}</span>
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
  return (
    <li
      className={`flex items-center [&+&]:border-t-[0.5px] [&+&]:border-line ${selected ? "bg-muted" : ""}`}
    >
      <a
        className="flex min-h-[60px] min-w-0 flex-1 items-center gap-3 py-2.5 pr-1 pl-3.5 text-inherit no-underline active:bg-muted focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent lg:min-h-14"
        href={`#/s/${s.id}`}
        aria-current={selected ? "page" : undefined}
      >
        <span className={`size-3 flex-none rounded-full ${dotColor[st.tone]}`} aria-hidden="true" />
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="text-[17px] font-semibold lg:text-[16px]">{s.name}</span>
          <span className="text-[15px] break-words text-label-2">
            <span className={`font-semibold ${textColor[st.tone]}`}>{st.label}</span>
            {s.kind !== "task" && !s.disabled && ` · ${ago(s.since)}`}
            <Counts s={s} />
          </span>
        </span>
        <svg
          className="flex-none text-label-2 lg:hidden"
          viewBox="0 0 24 24"
          width="20"
          height="20"
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
        className="mr-1 size-11 flex-none border-0 bg-transparent text-label-2 data-pressed:text-warn"
        pressed={starred}
        onPressedChange={() => toggleStar(s.id)}
        aria-label={`Star ${s.name}`}
      >
        <ToggleIcon data-variant="fill">
          <svg
            viewBox="0 0 24 24"
            width="20"
            height="20"
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
    if (only === "attention") {
      const tone = statusOf(s).tone;
      return tone === "warn" || tone === "bad";
    }
    return true;
  };
  const visible = services.filter(keep);
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
      <p className="mt-2.5 text-[15px] text-label-2">
        {only === "attention" ? "Nothing needs attention." : "No service matches."}
      </p>
    );
  }
  return (
    <>
      {sections.map((section) => (
        <section key={section.id} aria-label={section.label}>
          <h2 className={`${sectionClass} first:mt-0`}>{section.label}</h2>
          <ul className="m-0 list-none overflow-hidden rounded-[14px] bg-card p-0">
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
  const act = useAct();
  const ask = (title: string, message: string, label: string, agent: string) =>
    confirm({ title, message, label, agent });
  const stopped = s.kind === "process" && !s.want;
  const path = (what: string) => `services/${s.id}/${what}`;
  return (
    <section aria-label={`${s.name} actions`}>
      <h2 className={`${sectionClass} lg:mt-4`}>Actions</h2>
      <div className="grid gap-2.5 lg:flex lg:flex-wrap">
        {s.kind === "process" &&
          !s.disabled &&
          (stopped ? (
            <Button
              className={buttonClass}
              data-variant="solid"
              onClick={() =>
                act({
                  path: path("start"),
                  doing: `Starting ${s.name}`,
                  done: `${s.name} is starting`,
                })
              }
            >
              Start {s.name}
            </Button>
          ) : (
            <>
              <Button
                className={buttonClass}
                data-variant="solid"
                onClick={async () => {
                  if (
                    !(await ask(
                      `Restart ${s.name}?`,
                      "It rebuilds first. If the build fails, the running version stays up.",
                      `Restart ${s.name}`,
                      `Rebuilds and restarts ${s.name}`,
                    ))
                  )
                    return;
                  await act({
                    path: path("restart"),
                    doing: `Restarting ${s.name}`,
                    done: `${s.name} is restarting`,
                  });
                }}
              >
                Rebuild and restart
              </Button>
              <Button
                className={buttonClass}
                onClick={async () => {
                  if (
                    !(await ask(
                      `Stop ${s.name}?`,
                      "It stays stopped until you start it again.",
                      `Stop ${s.name}`,
                      `Stops ${s.name}`,
                    ))
                  )
                    return;
                  await act({
                    path: path("stop"),
                    doing: `Stopping ${s.name}`,
                    done: `${s.name} stopped`,
                  });
                }}
              >
                Stop {s.name}
              </Button>
            </>
          ))}
        {!s.disabled &&
          s.buttons.map((b) => (
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
            </Button>
          ))}
        {!s.disabled &&
          s.links.map((l) => (
            <a
              key={l.path}
              className="inline-flex min-h-[50px] w-full items-center justify-center rounded-xl border border-line bg-card px-4 text-[17px] font-semibold text-accent no-underline focus-visible:outline-2 focus-visible:outline-accent lg:min-h-10 lg:w-auto lg:text-[15px]"
              href={l.path.startsWith(":") ? `http://${location.hostname}${l.path}` : l.path}
              target="_blank"
              rel="noreferrer"
            >
              {l.label}
            </a>
          ))}
        {s.disabled ? (
          <Button
            className={buttonClass}
            onClick={() =>
              act({ path: path("enable"), doing: `Enabling ${s.name}`, done: `${s.name} enabled` })
            }
          >
            Enable {s.name}
          </Button>
        ) : (
          <Button
            className={`${buttonClass} text-bad`}
            onClick={async () => {
              const note =
                s.kind === "process"
                  ? "It stops and stays off, even after the hub restarts."
                  : "It stops being checked and its heartbeat stops, so whatever watches the heartbeat will alert.";
              if (
                !(await ask(`Disable ${s.name}?`, note, `Disable ${s.name}`, `Disables ${s.name}`))
              )
                return;
              await act({
                path: path("disable"),
                doing: `Disabling ${s.name}`,
                done: `${s.name} disabled`,
              });
            }}
          >
            Disable {s.name}
          </Button>
        )}
        {s.busy && (
          <p className="mt-2.5 text-[15px] text-label-2" role="status">
            Running “{s.busy}”…
          </p>
        )}
      </div>
    </section>
  );
}

export function Detail({ s, desktop }: { s: Service; services?: Service[]; desktop: boolean }) {
  const st = statusOf(s);
  return (
    <div className={desktop ? "flex min-h-0 flex-1 flex-col" : ""}>
      {!desktop && (
        <a
          className="-ml-1.5 inline-flex min-h-11 items-center gap-0.5 px-1.5 text-[17px] text-accent no-underline focus-visible:outline-2 focus-visible:outline-accent"
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
      <div className="lg:grid lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)] lg:items-start lg:gap-6">
        <div>
          <h1 className="mb-3.5 text-[34px] leading-[1.12] font-bold tracking-tight lg:mb-3 lg:text-[28px]">
            {s.name}
          </h1>
          <section className="rounded-[14px] bg-card px-4 py-3.5" aria-label="Status">
            <div className="flex flex-wrap items-center gap-2.5">
              <span
                className={`size-3 flex-none rounded-full ${dotColor[st.tone]}`}
                aria-hidden="true"
              />
              <span className={`text-xl font-semibold ${textColor[st.tone]}`}>{st.label}</span>
              {s.kind !== "task" && !s.disabled && (
                <span className="text-[15px] text-label-2">for {ago(s.since)}</span>
              )}
            </div>
            {s.note && <p className="mt-2.5 text-[15px] font-semibold text-warn">{s.note}</p>}
            {s.build && (
              <p className="mt-2.5 text-[15px] text-label-2">
                Build{" "}
                <span className={`font-semibold ${s.build.ok ? "text-good" : "text-bad"}`}>
                  {s.build.ok ? "passed" : "failed"}
                </span>{" "}
                in {seconds(s.build.ms)}, {ago(s.build.at)} ago.
              </p>
            )}
            {(s.errors > 0 || s.warnings > 0) && (
              <p className="mt-2.5 text-[15px]">
                <span className="font-semibold text-bad">{plural(s.errors, "error")}</span>,{" "}
                <span className="font-semibold text-warn">{plural(s.warnings, "warning")}</span>{" "}
                <span className="text-label-2">since the last start.</span>
              </p>
            )}
            {s.kind === "task" && s.detail && (
              <p className="mt-2.5 text-[15px] text-label-2">{s.detail}</p>
            )}
            <p className="mt-2.5 text-[15px] text-label-2">{s.desc}</p>
            {s.kind === "process" && s.restarts > 0 && (
              <p className="mt-2.5 text-[15px] text-label-2">
                Restarted {plural(s.restarts, "time")} since the hub started.
              </p>
            )}
          </section>
        </div>
        <Actions s={s} />
      </div>
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
