export type ButtonInfo = { id: string; label: string; confirm?: string };
export type LinkInfo = { label: string; path: string };
export type BuildInfo = { ok: boolean; at: number; ms: number };

export type Service = {
  id: string;
  name: string;
  group: string;
  kind: "process" | "check" | "task";
  desc: string;
  state: string;
  ready: boolean;
  detail?: string;
  note?: string;
  busy?: string;
  since: number;
  restarts: number;
  want: boolean;
  disabled: boolean;
  errors: number;
  warnings: number;
  build?: BuildInfo;
  blocker?: { port: number; pid: number; cmd: string };
  buttons: ButtonInfo[];
  links: LinkInfo[];
};

/** One log line: s is where it came from, l how serious it looks, n the service (combined log only). */
export type Line = {
  t: number;
  s: "build" | "run" | "hub";
  l: "info" | "warn" | "error";
  m: string;
  n?: string;
};

export type Tone = "good" | "warn" | "bad" | "idle";
export type Status = { label: string; tone: Tone };

/** Status is always words first; the color only repeats it. */
export function statusOf(s: Service): Status {
  if (s.disabled) return { label: "Disabled", tone: "idle" };
  if (s.kind === "task") {
    if (/\bON\b/.test(s.detail ?? "")) return { label: "On", tone: "good" };
    if (/\bOFF\b/.test(s.detail ?? "")) return { label: "Off", tone: "idle" };
    return { label: "Ready", tone: "idle" };
  }
  if (s.kind === "check") {
    if (s.state === "ok") return { label: "Running", tone: "good" };
    if (s.state === "failing") return { label: "Not responding", tone: "bad" };
    return { label: "Checking", tone: "idle" };
  }
  switch (s.state) {
    case "running":
      return s.ready
        ? { label: "Running", tone: "good" }
        : { label: "Not responding", tone: "warn" };
    case "starting":
      return { label: "Starting", tone: "warn" };
    case "building":
      return { label: "Building", tone: "warn" };
    case "blocked":
      return { label: "Port in use", tone: "warn" };
    case "backoff":
      return { label: "Crashed, retrying", tone: "bad" };
    case "failed":
      return { label: "Build failed", tone: "bad" };
    default:
      return { label: "Stopped", tone: "idle" };
  }
}

export async function getServices(): Promise<Service[]> {
  const res = await fetch("/api/services");
  if (!res.ok) throw new Error(String(res.status));
  return (await res.json()).services;
}

export type ActionResult = { ok: boolean; output?: string; error?: string };

export async function post(path: string): Promise<ActionResult> {
  const res = await fetch(`/api/${path}`, { method: "POST", headers: { "X-Ostler": "1" } });
  return res.json();
}

/** What the config file holds for one service: the page edits this, not the view. */
export type ServiceConfig = {
  id: string;
  name: string;
  group?: string;
  kind: "process" | "check" | "task";
  desc?: string;
  dir?: string;
  build?: string;
  run?: string;
  watch?: string[];
  ready?: string;
  port?: number;
  check?: string;
  interval?: number;
  heartbeat?: { url: string; method?: string; tokenEnv?: string };
  buttons?: { id: string; label: string; cmd: string; confirm?: string }[];
  links?: { label: string; path: string }[];
};

export type Meta = { canEdit: boolean; root?: string; urls?: string[] };

export async function getMeta(): Promise<Meta> {
  const res = await fetch("/api/meta");
  if (!res.ok) throw new Error(String(res.status));
  return res.json();
}

const HEADERS = { "X-Ostler": "1", "Content-Type": "application/json" };

export async function getConfig(id: string): Promise<ServiceConfig> {
  const res = await fetch(`/api/services/${encodeURIComponent(id)}/config`, { headers: HEADERS });
  if (!res.ok) throw new Error((await res.json().catch(() => ({}))).error ?? String(res.status));
  return res.json();
}

/** Adds (POST), changes (PUT) or deletes a service. Never throws: a failure comes back as ok: false. */
export async function saveService(
  method: "POST" | "PUT" | "DELETE",
  id: string | null,
  body?: ServiceConfig,
): Promise<ActionResult> {
  try {
    const res = await fetch(id ? `/api/services/${encodeURIComponent(id)}` : "/api/services", {
      method,
      headers: HEADERS,
      body: body ? JSON.stringify(body) : undefined,
    });
    return await res.json();
  } catch {
    return { ok: false, error: "Ostler didn't answer." };
  }
}

export function ago(since: number, now = Date.now() / 1000): string {
  const s = Math.max(0, Math.round(now - since));
  if (s < 60) return "just now";
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min`;
  const h = Math.floor(m / 60);
  if (h < 48) return `${h} h`;
  return `${Math.floor(h / 24)} d`;
}

export function seconds(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(ms < 10000 ? 1 : 0)} s`;
}

/** A service needs attention when its status is off, or it has logged an error since it started. */
export function needsAttention(s: Service): boolean {
  const tone = statusOf(s).tone;
  return tone === "warn" || tone === "bad" || s.errors > 0;
}

/** The tone a lamp shows: the status, raised to a warning when the service has logged an error. */
export function lampTone(s: Service): Tone {
  const tone = statusOf(s).tone;
  return (tone === "good" || tone === "idle") && s.errors > 0 ? "warn" : tone;
}

/** "for 2 min", but not "for just now". */
export function sinceText(since: number): string {
  const t = ago(since);
  return t === "just now" ? t : `for ${t}`;
}
