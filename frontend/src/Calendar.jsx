import React, { useEffect, useMemo, useState } from "react";
import { api } from "./api";

const MONTHS_ES = [
  "Enero", "Febrero", "Marzo", "Abril", "Mayo", "Junio",
  "Julio", "Agosto", "Septiembre", "Octubre", "Noviembre", "Diciembre",
];
const DOW_ES = ["Lun", "Mar", "Mié", "Jue", "Vie", "Sáb", "Dom"];

function isoMonday(d) {
  const day = (d.getDay() + 6) % 7; // 0 = Monday
  const m = new Date(d);
  m.setDate(d.getDate() - day);
  return m;
}
function toISO(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(
    d.getDate()
  ).padStart(2, "0")}`;
}

export default function Calendar({ onPlanned }) {
  const today = useMemo(() => new Date(), []);
  const [cursor, setCursor] = useState(() => isoMonday(today));
  const [weeks, setWeeks] = useState([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function loadWeeks() {
    try {
      setWeeks(await api.listWeeks());
    } catch (e) {
      setError(e.message);
    }
  }
  useEffect(() => {
    loadWeeks();
  }, []);

  const plannedSet = useMemo(() => new Set(weeks.map((w) => w.week_start)), [weeks]);

  // Build the visible month grid (weeks as rows).
  const monthStart = new Date(cursor.getFullYear(), cursor.getMonth(), 1);
  const gridStart = isoMonday(monthStart);
  const rows = [];
  for (let r = 0; r < 6; r++) {
    const row = [];
    for (let c = 0; c < 7; c++) {
      const d = new Date(gridStart);
      d.setDate(gridStart.getDate() + r * 7 + c);
      row.push(d);
    }
    rows.push(row);
  }

  async function planWeek(monday) {
    setError("");
    setBusy(true);
    try {
      const res = await api.createWeek(toISO(monday));
      await loadWeeks();
      onPlanned && onPlanned(res.id);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  function weekIsPlanned(monday) {
    return plannedSet.has(toISO(monday));
  }

  return (
    <div className="panel">
      <h2>Calendario</h2>
      <p className="muted">
        Elegí una semana y generá su plan. Cada semana va de lunes a domingo.
      </p>

      <div className="cal-head">
        <button className="link" onClick={() => setCursor(new Date(cursor.getFullYear(), cursor.getMonth() - 1, 1))}>
          ‹
        </button>
        <strong>
          {MONTHS_ES[cursor.getMonth()]} {cursor.getFullYear()}
        </strong>
        <button className="link" onClick={() => setCursor(new Date(cursor.getFullYear(), cursor.getMonth() + 1, 1))}>
          ›
        </button>
      </div>

      <table className="cal">
        <thead>
          <tr>
            <th className="wk"></th>
            {DOW_ES.map((d) => (
              <th key={d}>{d}</th>
            ))}
            <th></th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row, ri) => {
            const monday = row[0];
            const planned = weekIsPlanned(monday);
            const inMonth = row.some((d) => d.getMonth() === cursor.getMonth());
            if (!inMonth) return null;
            return (
              <tr key={ri} className={planned ? "planned-row" : ""}>
                <td className="wk muted">S{ri + 1}</td>
                {row.map((d, ci) => (
                  <td
                    key={ci}
                    className={
                      "day" +
                      (d.getMonth() === cursor.getMonth() ? "" : " out") +
                      (toISO(d) === toISO(today) ? " today" : "")
                    }
                  >
                    {d.getDate()}
                  </td>
                ))}
                <td>
                  {planned ? (
                    <span className="badge ok">Planificada</span>
                  ) : (
                    <button className="link" disabled={busy} onClick={() => planWeek(monday)}>
                      Planificar
                    </button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>

      {error && <div className="error">{error}</div>}
    </div>
  );
}
