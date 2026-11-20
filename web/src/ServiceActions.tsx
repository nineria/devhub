import { useQueryClient } from "@tanstack/react-query";
import { saveService, type Service } from "./api";
import { confirm, toast, useAct } from "./shared";

/** The things you can do to one service, shared by its buttons and its right-click menu. */
export function useServiceActions(s: Service) {
  const act = useAct();
  const client = useQueryClient();
  const remove = async () => {
    if (
      !(await confirm({
        title: `Delete ${s.name}?`,
        message: "It stops and is removed from ostler.json. This can't be undone.",
        label: `Delete ${s.name}`,
        agent: `Deletes ${s.name}`,
      }))
    )
      return;
    const res = await saveService("DELETE", s.id);
    if (res.ok) {
      toast(`${s.name} deleted`);
      location.hash = "#/";
    } else {
      toast(`Deleting ${s.name} didn't work`, { description: res.error });
    }
    await client.invalidateQueries({ queryKey: ["services"] });
  };
  const ask = (title: string, message: string, label: string, agent: string) =>
    confirm({ title, message, label, agent });
  const stopped = s.kind === "process" && !s.want;
  const path = (what: string) => `services/${s.id}/${what}`;
  const live = !s.disabled;

  const start = () =>
    act({
      path: path("start"),
      doing: `Starting ${s.name}`,
      done: `${s.name} is starting`,
    });
  const stop = () =>
    act({
      path: path("stop"),
      doing: `Stopping ${s.name}`,
      done: `${s.name} stopped`,
      undo: { label: "Undo", onAction: () => void start() },
    });
  const disable = async () => {
    const note =
      s.kind === "process"
        ? "It stops and stays off, even after Ostler restarts."
        : "It stops being checked and its heartbeat stops, so whatever watches the heartbeat will alert.";
    if (!(await ask(`Disable ${s.name}?`, note, `Disable ${s.name}`, `Disables ${s.name}`))) return;
    await act({
      path: path("disable"),
      doing: `Disabling ${s.name}`,
      done: `${s.name} disabled`,
    });
  };

  const restart = () =>
    act({
      path: path("restart"),
      doing: `Restarting ${s.name}`,
      done: `${s.name} is restarting`,
    });
  const enable = () =>
    act({ path: path("enable"), doing: `Enabling ${s.name}`, done: `${s.name} enabled` });
  return { act, ask, path, start, stop, disable, enable, restart, remove, stopped, live };
}
