const BASE = "/api";

async function req(path, opts) {
  const res = await fetch(BASE + path, {
    headers: { "Content-Type": "application/json" },
    ...opts,
  });
  if (!res.ok) {
    let detail = res.statusText;
    try {
      const body = await res.json();
      detail = body.detail || detail;
    } catch {
      /* ignore */
    }
    throw new Error(detail);
  }
  if (res.status === 204) return null;
  return res.json();
}

export const api = {
  getMap: () => req("/map"),

  listWorkers: () => req("/workers"),
  createWorker: (full_name) =>
    req("/workers", { method: "POST", body: JSON.stringify({ full_name }) }),
  deleteWorker: (id) => req(`/workers/${id}`, { method: "DELETE" }),
  workerSchedule: (id) => req(`/workers/${id}/schedule`),

  listWeeks: () => req("/weeks"),
  createWeek: (week_start) =>
    req("/weeks", { method: "POST", body: JSON.stringify({ week_start }) }),
  getWeek: (id) => req(`/weeks/${id}`),
  replanWeek: (id) => req(`/weeks/${id}/replan`, { method: "POST" }),
  deleteWeek: (id) => req(`/weeks/${id}`, { method: "DELETE" }),

  getSettings: () => req("/settings"),
  setDB: (path, mode) =>
    req("/settings/db", { method: "POST", body: JSON.stringify({ path, mode }) }),
  browseDB: (mode) =>
    req("/settings/browse", { method: "POST", body: JSON.stringify({ mode }) }),
};
