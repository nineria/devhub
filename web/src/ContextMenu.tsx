import { useEffect, useRef, useState } from "react";
import type { Service } from "./api";
import { useServiceActions } from "./ServiceActions";
import { useEditor, useMeta } from "./shared";

type Open = { x: number; y: number; id: string | null };

type Item = { label: string; bad?: boolean; run: () => void };

/** Fields and selected text keep the browser's own menu, so Copy and Paste still work there. */
function wantsNativeMenu(target: EventTarget | null): boolean {
  if (!(target instanceof Element)) return true;
  if (target.closest("input, textarea, select, [contenteditable='true'], [role='dialog']"))
    return true;
  const selected = window.getSelection();
  return !!selected && !selected.isCollapsed && selected.toString().trim() !== "";
}

/** A right-click menu for the whole page: on a service it lists that service's actions, elsewhere the page's. */
export function ContextMenu({ services }: { services: Service[] }) {
  const [open, setOpen] = useState<Open | null>(null);

  useEffect(() => {
    const onMenu = (e: MouseEvent) => {
      if (wantsNativeMenu(e.target)) return;
      e.preventDefault();
      const el = (e.target as Element).closest<HTMLElement>("[data-service]");
      setOpen({ x: e.clientX, y: e.clientY, id: el?.dataset.service ?? null });
    };
    document.addEventListener("contextmenu", onMenu);
    return () => document.removeEventListener("contextmenu", onMenu);
  }, []);

  if (!open) return null;
  const s = open.id ? services.find((x) => x.id === open.id) : undefined;
  const close = () => setOpen(null);
  return s ? (
    <ForService key={s.id} s={s} at={open} close={close} />
  ) : (
    <ForPage at={open} close={close} />
  );
}

function ForPage({ at, close }: { at: Open; close: () => void }) {
  const canEdit = useMeta().data?.canEdit === true;
  const openEditor = useEditor();
  const items: Item[] = [];
  if (canEdit) items.push({ label: "New service", run: () => openEditor() });
  items.push({ label: "All logs", run: () => (location.hash = "#/logs") });
  items.push({ label: "Reload", run: () => location.reload() });
  return <Popup at={at} close={close} items={items} />;
}

function ForService({ s, at, close }: { s: Service; at: Open; close: () => void }) {
  const canEdit = useMeta().data?.canEdit === true;
  const openEditor = useEditor();
  const a = useServiceActions(s);
  const items: Item[] = [{ label: "Open", run: () => (location.hash = `#/s/${s.id}`) }];
  if (s.kind === "process" && a.live) {
    items.push(
      a.stopped
        ? { label: "Start", run: () => void a.start() }
        : { label: "Rebuild and restart", run: () => void a.restart() },
    );
    if (!a.stopped) items.push({ label: "Stop", run: () => void a.stop() });
  }
  if (a.live) {
    for (const b of s.buttons) {
      items.push({
        label: b.label,
        run: async () => {
          if (
            b.confirm &&
            !(await a.ask(`${b.label}?`, b.confirm, b.label, `${b.label} on ${s.name}`))
          )
            return;
          await a.act({
            path: a.path(`buttons/${b.id}`),
            doing: b.label,
            done: `${b.label}: done`,
          });
        },
      });
    }
  }
  if (s.disabled) items.push({ label: "Enable", run: () => void a.enable() });
  if (canEdit) items.push({ label: "Edit", run: () => openEditor(s.id) });
  if (a.live) items.push({ label: "Disable", bad: true, run: () => void a.disable() });
  if (canEdit) items.push({ label: "Delete", bad: true, run: () => void a.remove() });
  return <Popup at={at} close={close} items={items} title={s.name} />;
}

function Popup({
  at,
  items,
  close,
  title,
}: {
  at: Open;
  items: Item[];
  close: () => void;
  title?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ left: at.x, top: at.y });

  // Keep the menu inside the window, and give it focus so the arrow keys work.
  useEffect(() => {
    const box = ref.current;
    if (!box) return;
    const r = box.getBoundingClientRect();
    setPos({
      left: Math.max(4, Math.min(at.x, window.innerWidth - r.width - 4)),
      top: Math.max(4, Math.min(at.y, window.innerHeight - r.height - 4)),
    });
    box.querySelector<HTMLButtonElement>("button")?.focus();
  }, [at.x, at.y]);

  useEffect(() => {
    const out = (e: Event) => {
      if (!ref.current?.contains(e.target as Node)) close();
    };
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") close();
    };
    document.addEventListener("pointerdown", out, true);
    document.addEventListener("contextmenu", out, true);
    document.addEventListener("keydown", key);
    window.addEventListener("blur", close);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", close, true);
    return () => {
      document.removeEventListener("pointerdown", out, true);
      document.removeEventListener("contextmenu", out, true);
      document.removeEventListener("keydown", key);
      window.removeEventListener("blur", close);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", close, true);
    };
  }, [close]);

  const move = (e: React.KeyboardEvent) => {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    const all = [...(ref.current?.querySelectorAll<HTMLButtonElement>("button") ?? [])];
    const i = all.indexOf(document.activeElement as HTMLButtonElement);
    const next = e.key === "ArrowDown" ? i + 1 : i - 1;
    all[(next + all.length) % all.length]?.focus();
  };

  return (
    <div
      ref={ref}
      role="menu"
      aria-label={title ? `Actions for ${title}` : "Page actions"}
      onKeyDown={move}
      style={{ left: pos.left, top: pos.top }}
      className="fixed z-50 min-w-48 rounded-(--radius-card) border border-line bg-card p-1 shadow-lg"
    >
      {title && (
        <div className="truncate px-3 py-1.5 text-[12px] font-semibold text-label-2">{title}</div>
      )}
      {items.map((it) => (
        <button
          key={it.label}
          role="menuitem"
          type="button"
          className={`flex w-full cursor-default items-center rounded-(--radius-control) border-0 bg-transparent px-3 py-1.5 text-left text-[14px] hover:bg-muted focus-visible:bg-muted focus-visible:outline-none ${it.bad ? "text-bad" : "text-inherit"}`}
          onClick={() => {
            close();
            it.run();
          }}
        >
          {it.label}
        </button>
      ))}
    </div>
  );
}
