import { useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@ui-sdk/react/button";
import {
  Dialog,
  DialogBackdrop,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@ui-sdk/react/dialog";
import { Field, FieldDescription, FieldLabel } from "@ui-sdk/react/field";
import { Input } from "@ui-sdk/react/input";
import {
  SegmentedControl,
  SegmentedControlIndicator,
  SegmentedControlItem,
} from "@ui-sdk/react/segmented-control";
import { getConfig, saveService, type ServiceConfig } from "./api";
import { sectionClass, toast, useMeta } from "./shared";

type Kind = ServiceConfig["kind"];

type Draft = {
  id: string;
  name: string;
  group: string;
  kind: Kind;
  desc: string;
  dir: string;
  build: string;
  run: string;
  watch: string;
  ready: string;
  port: string;
  check: string;
  interval: string;
  hbUrl: string;
  hbMethod: string;
  hbToken: string;
  buttons: { id: string; label: string; cmd: string; confirm: string }[];
  links: { label: string; path: string }[];
};

const blank: Draft = {
  id: "",
  name: "",
  group: "",
  kind: "process",
  desc: "",
  dir: "",
  build: "",
  run: "",
  watch: "",
  ready: "",
  port: "",
  check: "",
  interval: "30",
  hbUrl: "",
  hbMethod: "",
  hbToken: "",
  buttons: [],
  links: [],
};

function toDraft(c: ServiceConfig): Draft {
  return {
    id: c.id,
    name: c.name,
    group: c.group ?? "",
    kind: c.kind,
    desc: c.desc ?? "",
    dir: c.dir ?? "",
    build: c.build ?? "",
    run: c.run ?? "",
    watch: (c.watch ?? []).join("\n"),
    ready: c.ready ?? "",
    port: c.port ? String(c.port) : "",
    check: c.check ?? "",
    interval: String(c.interval ?? 30),
    hbUrl: c.heartbeat?.url ?? "",
    hbMethod: c.heartbeat?.method ?? "",
    hbToken: c.heartbeat?.tokenEnv ?? "",
    buttons: (c.buttons ?? []).map((b) => ({ ...b, confirm: b.confirm ?? "" })),
    links: c.links ?? [],
  };
}

function toConfig(d: Draft): ServiceConfig {
  return {
    id: d.id,
    name: d.name,
    group: d.group,
    kind: d.kind,
    desc: d.desc,
    dir: d.dir,
    build: d.build,
    run: d.run,
    watch: d.watch.split("\n"),
    ready: d.ready,
    port: Math.max(0, Math.round(Number(d.port) || 0)),
    check: d.check,
    interval: Math.max(0, Math.round(Number(d.interval) || 0)),
    heartbeat: d.hbUrl.trim()
      ? { url: d.hbUrl, method: d.hbMethod, tokenEnv: d.hbToken }
      : undefined,
    buttons: d.buttons,
    links: d.links,
  };
}

const slug = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");

const KINDS: { id: Kind; label: string; hint: string }[] = [
  {
    id: "process",
    label: "Process",
    hint: "A long-running command that Ostler starts, restarts and rebuilds.",
  },
  {
    id: "check",
    label: "Check",
    hint: "A command that runs every few seconds. Exit 0 means healthy.",
  },
  { id: "task", label: "Task", hint: "A command that runs on demand, with buttons." },
];

function Text({
  label,
  hint,
  value,
  onChange,
  required,
  readOnly,
  placeholder,
  list,
  mono,
  type,
}: {
  label: string;
  hint?: ReactNode;
  value: string;
  onChange: (v: string) => void;
  required?: boolean;
  readOnly?: boolean;
  placeholder?: string;
  list?: string;
  mono?: boolean;
  type?: string;
}) {
  return (
    <Field>
      <FieldLabel>{label}</FieldLabel>
      <Input
        value={value}
        onValueChange={(v) => onChange(v)}
        required={required}
        readOnly={readOnly}
        placeholder={placeholder}
        list={list}
        type={type}
        autoComplete="off"
        autoCapitalize="off"
        spellCheck={false}
        className={mono ? "font-mono" : undefined}
      />
      {hint && <FieldDescription>{hint}</FieldDescription>}
    </Field>
  );
}

function Section({
  title,
  children,
  cols,
}: {
  title: string;
  children: ReactNode;
  cols?: boolean;
}) {
  return (
    <fieldset className={`m-0 grid gap-3 border-0 p-0 ${cols ? "lg:grid-cols-2" : ""}`}>
      <legend className={`${sectionClass} mb-2 p-0`}>{title}</legend>
      {children}
    </fieldset>
  );
}

const rowClass = "grid gap-2 rounded-(--radius-card) border border-line p-3";

function Form({
  initial,
  editing,
  groups,
  root,
  onClose,
}: {
  initial: Draft;
  editing: boolean;
  groups: string[];
  root?: string;
  onClose: () => void;
}) {
  const client = useQueryClient();
  const [d, setD] = useState(initial);
  const [idTouched, setIdTouched] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const set = <K extends keyof Draft>(k: K, v: Draft[K]) => setD((p) => ({ ...p, [k]: v }));
  const process = d.kind === "process";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    const res = await saveService(editing ? "PUT" : "POST", editing ? d.id : null, toConfig(d));
    setBusy(false);
    if (!res.ok) {
      setError(res.error ?? "Saving didn't work.");
      return;
    }
    toast(editing ? `${d.name} saved` : `${d.name} added`, {
      description: editing ? "It was restarted with the new settings." : undefined,
    });
    await client.invalidateQueries({ queryKey: ["services"] });
    onClose();
    if (!editing) location.hash = `#/s/${d.id}`;
  }

  return (
    <form onSubmit={submit} className="grid gap-5" noValidate={false}>
      <div data-slot="stack">
        <DialogTitle>{editing ? `Edit ${initial.name}` : "New service"}</DialogTitle>
        <DialogDescription>
          {editing
            ? "Saving writes ostler.json and restarts this service with the new settings."
            : "Saving writes it to ostler.json and starts it."}
        </DialogDescription>
      </div>

      <SegmentedControl
        aria-label="Kind"
        value={d.kind}
        onValueChange={(v) => set("kind", v as Kind)}
      >
        <SegmentedControlIndicator />
        {KINDS.map((k) => (
          <SegmentedControlItem key={k.id} value={k.id}>
            {k.label}
          </SegmentedControlItem>
        ))}
      </SegmentedControl>
      <p className="m-0 -mt-2 text-[13px] text-label-2">
        {KINDS.find((k) => k.id === d.kind)?.hint}
      </p>

      <Section title="General" cols>
        <Text
          label="Name"
          required
          value={d.name}
          onChange={(v) => {
            set("name", v);
            if (!editing && !idTouched) set("id", slug(v));
          }}
        />
        {editing ? null : (
          <Text
            label="ID"
            required
            mono
            value={d.id}
            onChange={(v) => {
              setIdTouched(true);
              set("id", v);
            }}
            hint="Used in the page address and the saved state. It can't be changed later."
          />
        )}
        <Text
          label="Group"
          value={d.group}
          onChange={(v) => set("group", v)}
          list="ostler-groups"
          hint="Services with the same group are listed together."
        />
        <datalist id="ostler-groups">
          {groups.map((g) => (
            <option key={g} value={g} />
          ))}
        </datalist>
        <Text label="Description" value={d.desc} onChange={(v) => set("desc", v)} />
        <Text
          label="Folder"
          mono
          value={d.dir}
          onChange={(v) => set("dir", v)}
          hint={root ? <>Relative to {root}. Commands run here.</> : "Relative to the root folder."}
        />
      </Section>

      {process ? (
        <Section title="Process">
          <Text
            label="Run command"
            required
            mono
            value={d.run}
            onChange={(v) => set("run", v)}
            placeholder="./bin/api"
            hint="Keep it in the foreground."
          />
          <Text
            label="Build command"
            mono
            value={d.build}
            onChange={(v) => set("build", v)}
            placeholder="go build -o bin/api ./cmd/api"
            hint="Runs before the service and after each change. If it fails, the old version keeps running."
          />
          <Field>
            <FieldLabel>Watch</FieldLabel>
            <textarea
              data-slot="input"
              className="min-h-20 w-full resize-y px-3 py-2 font-mono"
              style={{ height: "auto" }}
              rows={3}
              value={d.watch}
              onChange={(e) => set("watch", e.target.value)}
              placeholder={"my-project/cmd\nmy-project/internal"}
              spellCheck={false}
              aria-describedby="watch-hint"
            />
            <p id="watch-hint" className="m-0 mt-1 text-[13px] text-label-2">
              One folder or file per line, relative to the root. A change rebuilds and restarts.
            </p>
          </Field>
          <Text
            label="Port"
            type="number"
            value={d.port}
            onChange={(v) => set("port", v)}
            placeholder="8765"
            hint="Optional. If another process is using it, Ostler waits and offers to stop that process, instead of crashing in a loop."
          />
          <Text
            label="Ready URL"
            mono
            value={d.ready}
            onChange={(v) => set("ready", v)}
            placeholder="http://127.0.0.1:8080/healthz"
            hint="Counts as running once this answers with a status below 400."
          />
        </Section>
      ) : (
        <Section title={d.kind === "check" ? "Check" : "Status command"}>
          <Text
            label="Command"
            required
            mono
            value={d.check}
            onChange={(v) => set("check", v)}
            placeholder="pg_isready -q -h 127.0.0.1"
            hint={
              d.kind === "check"
                ? "Exit 0 means healthy."
                : "The first line of its output is shown."
            }
          />
          {d.kind === "check" && (
            <>
              <Text
                label="Every (seconds)"
                type="number"
                value={d.interval}
                onChange={(v) => set("interval", v)}
                hint="0 checks once."
              />
              <Text
                label="Heartbeat URL"
                mono
                value={d.hbUrl}
                onChange={(v) => set("hbUrl", v)}
                placeholder="https://hc-ping.com/${HEALTHCHECK_UUID}"
                hint="Sent after each passing check. ${NAME} is filled from your env files."
              />
              {d.hbUrl.trim() && (
                <>
                  <Text
                    label="Heartbeat method"
                    mono
                    value={d.hbMethod}
                    onChange={(v) => set("hbMethod", v)}
                    placeholder="POST"
                  />
                  <Text
                    label="Bearer token variable"
                    mono
                    value={d.hbToken}
                    onChange={(v) => set("hbToken", v)}
                    hint="Name of the variable whose value is sent as a bearer token."
                  />
                </>
              )}
            </>
          )}
        </Section>
      )}

      {!process && (
        <Section title="Buttons">
          {d.buttons.map((b, i) => {
            const upd = (patch: Partial<Draft["buttons"][number]>) =>
              set(
                "buttons",
                d.buttons.map((x, j) => (j === i ? { ...x, ...patch } : x)),
              );
            return (
              <div key={i} className={rowClass}>
                <Text label="Label" value={b.label} onChange={(v) => upd({ label: v })} />
                <Text label="Command" mono value={b.cmd} onChange={(v) => upd({ cmd: v })} />
                <Text
                  label="Ask first"
                  value={b.confirm}
                  onChange={(v) => upd({ confirm: v })}
                  hint="Optional. Shown in a question before the button runs."
                />
                <Button
                  type="button"
                  className="justify-self-start text-bad"
                  onClick={() =>
                    set(
                      "buttons",
                      d.buttons.filter((_, j) => j !== i),
                    )
                  }
                >
                  Remove button
                </Button>
              </div>
            );
          })}
          <Button
            type="button"
            className="justify-self-start"
            onClick={() =>
              set("buttons", [...d.buttons, { id: "", label: "", cmd: "", confirm: "" }])
            }
          >
            Add button
          </Button>
        </Section>
      )}

      <Section title="Links">
        {d.links.map((l, i) => {
          const upd = (patch: Partial<Draft["links"][number]>) =>
            set(
              "links",
              d.links.map((x, j) => (j === i ? { ...x, ...patch } : x)),
            );
          return (
            <div key={i} className={rowClass}>
              <Text label="Label" value={l.label} onChange={(v) => upd({ label: v })} />
              <Text
                label="Address"
                mono
                value={l.path}
                onChange={(v) => upd({ path: v })}
                placeholder=":8080"
                hint="A port like :8080 opens that port on the host you opened Ostler from."
              />
              <Button
                type="button"
                className="justify-self-start text-bad"
                onClick={() =>
                  set(
                    "links",
                    d.links.filter((_, j) => j !== i),
                  )
                }
              >
                Remove link
              </Button>
            </div>
          );
        })}
        <Button
          type="button"
          className="justify-self-start"
          onClick={() => set("links", [...d.links, { label: "", path: "" }])}
        >
          Add link
        </Button>
      </Section>

      {error && (
        <p className="m-0 text-[15px] font-semibold text-bad lg:text-[13px]" role="alert">
          {error}
        </p>
      )}
      <div
        data-slot="row"
        className="sticky bottom-0 -mx-5 -mb-5 border-t border-line bg-card px-5 py-3"
      >
        <DialogClose type="button">Cancel</DialogClose>
        <Button type="submit" data-variant="solid" disabled={busy}>
          {busy ? "Saving…" : editing ? "Save and restart" : "Add service"}
        </Button>
      </div>
    </form>
  );
}

/** The add and edit dialog. `target` is null when closed, `{}` for a new service, `{ id }` to edit one. */
export function ServiceEditor({
  target,
  groups,
  onClose,
}: {
  target: { id?: string; group?: string } | null;
  groups: string[];
  onClose: () => void;
}) {
  const meta = useMeta();
  const id = target?.id;
  const config = useQuery({
    queryKey: ["config", id],
    queryFn: () => getConfig(id!),
    enabled: !!id,
    gcTime: 0,
    staleTime: 0,
  });
  return (
    <Dialog open={target !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogBackdrop />
      <DialogContent className="service-editor">
        {id && config.isPending ? (
          <>
            <DialogTitle>Edit service</DialogTitle>
            <DialogDescription role="status">Loading…</DialogDescription>
          </>
        ) : id && config.isError ? (
          <>
            <DialogTitle>Edit service</DialogTitle>
            <DialogDescription role="alert">
              Can't read this service: {(config.error as Error).message}
            </DialogDescription>
            <div data-slot="row">
              <DialogClose>Close</DialogClose>
            </div>
          </>
        ) : target ? (
          <Form
            key={id ?? `new:${target.group ?? ""}`}
            initial={
              id && config.data ? toDraft(config.data) : { ...blank, group: target.group ?? "" }
            }
            editing={!!id}
            groups={groups}
            root={meta.data?.root}
            onClose={onClose}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}
