import { memo, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Button } from "@ui-sdk/react/button";
import { Input } from "@ui-sdk/react/input";
import {
  SegmentedControl,
  SegmentedControlIndicator,
  SegmentedControlItem,
} from "@ui-sdk/react/segmented-control";
import { Switch, SwitchThumb } from "@ui-sdk/react/switch";
import { post, type Line } from "./api";
import { copyText, sectionClass, toast } from "./shared";

// Terminal color codes from the services' own output; built from the escape character on purpose.
const ANSI = new RegExp(`${String.fromCharCode(27)}\\[[0-9;]*[A-Za-z]`, "g");

/** Streams a log from Ostler. Lines are batched so a chatty service doesn't re-render the page per line. */
function useLogStream(url: string, cap: number) {
  const [lines, setLines] = useState<Line[] | null>(null);
  // Every line received, so "new lines" can still be counted once the buffer is full and stops growing.
  const [received, setReceived] = useState(0);
  useEffect(() => {
    setLines(null);
    setReceived(0);
    const es = new EventSource(url);
    let pending: Line[] = [];
    let first = true;
    const flush = setInterval(() => {
      if (pending.length === 0 && !first) return;
      const batch = pending;
      pending = [];
      first = false;
      setReceived((n) => n + batch.length);
      setLines((prev) => [...(prev ?? []), ...batch].slice(-cap));
    }, 400);
    es.onmessage = (e) => {
      const line = JSON.parse(e.data) as Line;
      line.m = line.m.replace(ANSI, "");
      pending.push(line);
    };
    return () => {
      es.close();
      clearInterval(flush);
    };
  }, [url, cap]);
  return { lines, received, clear: () => setLines([]) };
}

/**
 * "/pattern/" is a regular expression; anything else is a plain, case-insensitive match.
 * `invalid` is set when the text is written as /pattern/ but doesn't compile, so the page can say
 * that it is matching the text literally instead.
 */
function toRegExp(text: string): { re: RegExp | null; invalid: boolean } {
  const t = text.trim();
  if (!t) return { re: null, invalid: false };
  let invalid = false;
  if (t.length > 2 && t.startsWith("/") && t.endsWith("/")) {
    try {
      return { re: new RegExp(t.slice(1, -1), "gi"), invalid: false };
    } catch {
      invalid = true;
    }
  }
  return { re: new RegExp(t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "gi"), invalid };
}

const clock = (t: number) => new Date(t).toLocaleTimeString([], { hour12: false });
const clockMs = (t: number) => `${clock(t)}.${String(t % 1000).padStart(3, "0")}`;

// A service's own date stamp repeats the time column, so it is dropped from the message.
const STAMP = /^\d{4}[/-]\d{2}[/-]\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\s+/;
const LEVEL_WORD = /^(?:INFO|WARN|WARNING|ERROR|DEBUG|TRACE)\b:?\s*/;
const tidy = (m: string) => m.replace(STAMP, "").replace(LEVEL_WORD, "");

/** Wraps every match in a highlight, so the search term shows in the line. */
function highlight(text: string, re: RegExp | null): ReactNode {
  if (!re) return text;
  const parts: ReactNode[] = [];
  let last = 0;
  let n = 0;
  for (const match of text.matchAll(re)) {
    if (match[0] === "") continue;
    const at = match.index ?? 0;
    parts.push(text.slice(last, at));
    parts.push(
      <mark key={n++} className="rounded-sm bg-focus/30 text-inherit">
        {match[0]}
      </mark>,
    );
    last = at + match[0].length;
  }
  parts.push(text.slice(last));
  return parts;
}

type Level = "all" | "error" | "warn";
type Source = "all" | "build" | "run";

const rowTint = {
  info: "hover:bg-muted/60",
  warn: "bg-warn/12",
  error: "bg-bad/12",
} as const;

/** One log row: time, level, source, message. A click opens it in full with its raw details. */
const Row = memo(function Row({
  l,
  service,
  wrap,
  re,
}: {
  l: Line;
  service?: string;
  wrap: boolean;
  re: RegExp | null;
}) {
  const [open, setOpen] = useState(false);
  const text = useMemo(() => tidy(l.m), [l.m]);
  const full = wrap || open;
  return (
    <div
      role="row"
      tabIndex={0}
      aria-expanded={open}
      onClick={() => !window.getSelection()?.toString() && setOpen((o) => !o)}
      onKeyDown={(e) =>
        (e.key === "Enter" || e.key === " ") && (e.preventDefault(), setOpen((o) => !o))
      }
      className={`cursor-default border-b-[0.5px] border-b-line/60 px-3 py-[3px] outline-none focus-visible:bg-muted ${rowTint[l.l]}`}
    >
      <div className="flex flex-wrap items-baseline gap-x-3 lg:flex-nowrap">
        <span className="w-[5.5rem] flex-none text-label-2 tabular-nums select-none">
          {clock(l.t)}
        </span>
        {service !== undefined && (
          <span className="w-28 flex-none truncate font-semibold select-none">{service}</span>
        )}
        <span className="w-[3.25rem] flex-none text-[11px] font-semibold select-none">
          {l.l === "error" ? (
            <span className="font-bold text-bad">error</span>
          ) : l.l === "warn" ? (
            <span className="font-bold text-warn">warn</span>
          ) : (
            <span className="text-label-2">{l.s}</span>
          )}
        </span>
        <span
          className={`min-w-0 basis-full lg:basis-0 lg:flex-1 ${full ? "break-words whitespace-pre-wrap" : "truncate"} ${l.s === "hub" && l.l === "info" ? "text-label-2" : ""}`}
        >
          {highlight(text, re)}
        </span>
      </div>
      {open && (
        <div className="mt-1 mb-1.5 rounded-(--radius-control) bg-bg px-2.5 py-2 text-[12px] text-label-2">
          <div>
            {clockMs(l.t)} · {l.s}
            {service ? ` · ${service}` : ""} · {l.l}
          </div>
          <div className="mt-1 break-words whitespace-pre-wrap text-label">{l.m}</div>
        </div>
      )}
    </div>
  );
});

export function LogView({
  url,
  clearPath,
  names,
  fill,
  title = "Log",
}: {
  url: string;
  clearPath: string;
  names?: Record<string, string>;
  fill?: boolean;
  title?: string;
}) {
  const combined = names !== undefined;
  const { lines, received, clear } = useLogStream(url, combined ? 4000 : 2000);
  const [pausedAt, setPausedAt] = useState(0);
  const [text, setText] = useState("");
  const [level, setLevel] = useState<Level>("all");
  const [source, setSource] = useState<Source>("all");
  const [follow, setFollow] = useState(true);
  const [wrap, setWrap] = useState(() => !window.matchMedia("(min-width: 1024px)").matches);
  const box = useRef<HTMLDivElement>(null);

  const { re, invalid } = useMemo(() => toRegExp(text), [text]);
  const shown = useMemo(() => {
    return (lines ?? []).filter((l) => {
      if (l.m.trim() === "") return false; // blank lines add nothing
      if (level !== "all" && l.l !== level) return false;
      if (source !== "all" && l.s !== source) return false;
      if (!re) return true;
      re.lastIndex = 0;
      return re.test(`${l.n ? (names?.[l.n] ?? l.n) + " " : ""}${l.m}`);
    });
  }, [lines, re, level, source, names]);

  const errors = (lines ?? []).filter((l) => l.l === "error").length;
  const warnings = (lines ?? []).filter((l) => l.l === "warn").length;

  useEffect(() => {
    if (follow && box.current) box.current.scrollTop = box.current.scrollHeight;
  }, [shown, follow, wrap]);

  // Remember how many lines had arrived when following paused, to show how many came since.
  useEffect(() => {
    if (!follow) setPausedAt(received);
    // Only the moment following pauses matters, not every later line.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [follow]);
  const fresh = follow ? 0 : Math.max(0, received - pausedAt);

  // Scrolling up pauses following; reaching the bottom again resumes it, like a live log.
  const onScroll = () => {
    const el = box.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 32;
    if (!atBottom && follow) setFollow(false);
    else if (atBottom && !follow) setFollow(true);
  };

  const plain = () =>
    shown.map((l) => `${clock(l.t)} ${l.n ? `[${names?.[l.n] ?? l.n}] ` : ""}${l.m}`).join("\n");

  return (
    <section aria-label={title} className={fill ? "flex min-h-0 flex-1 flex-col" : ""}>
      <div className="flex flex-wrap items-center justify-between gap-x-4">
        <h2 className={sectionClass}>{title}</h2>
        <div className="flex items-center gap-4">
          <label className="inline-flex min-h-11 items-center gap-2.5 text-[15px] text-label-2 lg:text-[13px]">
            Wrap
            <Switch checked={wrap} onCheckedChange={setWrap} aria-label="Wrap long lines">
              <SwitchThumb />
            </Switch>
          </label>
          <label className="inline-flex min-h-11 items-center gap-2.5 text-[15px] text-label-2 lg:text-[13px]">
            <span className="inline-flex items-center gap-1.5">
              <span
                className={`size-2 rounded-full ${follow ? "bg-good" : "bg-label-2"}`}
                aria-hidden="true"
              />
              Live
            </span>
            <Switch
              checked={follow}
              onCheckedChange={setFollow}
              aria-label="Follow the newest log line"
            >
              <SwitchThumb />
            </Switch>
          </label>
        </div>
      </div>

      {invalid && (
        <p id="log-pattern-hint" role="status" className="m-0 mb-1.5 text-[13px] text-warn">
          Not a valid pattern, so this matches the text as typed.
        </p>
      )}
      <div className="mb-2 grid gap-2 lg:flex lg:flex-wrap lg:items-center">
        <Input
          className="lg:w-64"
          aria-label="Filter log lines"
          placeholder="Filter, or /regex/"
          value={text}
          onValueChange={(v) => setText(v)}
          aria-invalid={invalid || undefined}
          aria-describedby={invalid ? "log-pattern-hint" : undefined}
        />
        <SegmentedControl
          aria-label="Level"
          value={level}
          onValueChange={(v) => setLevel(v as Level)}
        >
          <SegmentedControlIndicator />
          <SegmentedControlItem value="all">All</SegmentedControlItem>
          <SegmentedControlItem value="error">Errors ({errors})</SegmentedControlItem>
          <SegmentedControlItem value="warn">Warnings ({warnings})</SegmentedControlItem>
        </SegmentedControl>
        <SegmentedControl
          aria-label="Source"
          value={source}
          onValueChange={(v) => setSource(v as Source)}
        >
          <SegmentedControlIndicator />
          <SegmentedControlItem value="all">All sources</SegmentedControlItem>
          <SegmentedControlItem value="build">Build</SegmentedControlItem>
          <SegmentedControlItem value="run">Runtime</SegmentedControlItem>
        </SegmentedControl>
        <div className="flex gap-2 lg:ml-auto">
          <Button
            className="flex-1 lg:flex-none lg:px-3.5"
            onClick={() =>
              toast(copyText(plain()) ? `Copied ${shown.length} lines` : "Couldn't copy")
            }
          >
            Copy
          </Button>
          <Button
            className="flex-1 lg:flex-none lg:px-3.5"
            onClick={async () => {
              const res = await post(clearPath);
              if (res.ok) clear();
              else toast("Couldn't clear the log", { description: res.error });
            }}
          >
            Clear
          </Button>
        </div>
      </div>

      <div
        className={`relative overflow-hidden rounded-(--radius-card) border border-line bg-card ${fill ? "flex min-h-0 flex-1 flex-col" : ""}`}
      >
        <div
          ref={box}
          onScroll={onScroll}
          role="log"
          aria-label={`${title} lines`}
          className={`overflow-auto py-1 font-mono text-[12px] leading-[1.6] lg:text-[11.5px] ${fill ? "min-h-0 flex-1" : "max-h-[55vh] lg:max-h-[60vh]"}`}
        >
          {lines === null ? (
            <p className="px-3 py-2 text-label-2">Loading…</p>
          ) : shown.length === 0 ? (
            <p className="px-3 py-2 text-label-2">
              {lines.length === 0 ? "Nothing logged yet." : "No lines match the filter."}
            </p>
          ) : (
            shown.map((l, i) => (
              <Row
                key={`${l.t}-${i}`}
                l={l}
                service={combined ? (names?.[l.n ?? ""] ?? l.n) : undefined}
                wrap={wrap}
                re={re}
              />
            ))
          )}
        </div>
        {!follow && shown.length > 0 && (
          <Button
            className="absolute right-3 bottom-3 px-4 shadow-lg"
            data-variant="solid"
            onClick={() => setFollow(true)}
          >
            {fresh > 0 ? `Jump to latest (${fresh > 999 ? "999+" : fresh} new)` : "Jump to latest"}
          </Button>
        )}
      </div>
      <p className="mt-1.5 px-1 text-[13px] text-label-2">
        {lines === null
          ? ""
          : shown.length === lines.length
            ? `${lines.length} lines`
            : `${shown.length} of ${lines.length} lines`}
      </p>
    </section>
  );
}
