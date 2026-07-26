import React, { useEffect, useMemo, useState } from "react";
import { api } from "./api";
import StreetMap from "./StreetMap";
import { downloadWeek, downloadWorkerDay } from "./mapExport";

function activeAt(shiftDef, hour) {
  return shiftDef.intervals.some(([lo, hi]) => hour >= lo && hour < hi);
}
function fmtHour(h) {
  const hh = Math.floor(h);
  const mm = Math.round((h - hh) * 60);
  return `${String(hh).padStart(2, "0")}:${String(mm).padStart(2, "0")}`;
}

export default function PlanView({ map, selectedId, onSelect }) {
  const [weeks, setWeeks] = useState([]);
  const [plan, setPlan] = useState(null);
  const [dayIdx, setDayIdx] = useState(0);
  const [hour, setHour] = useState(10);
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

  async function openWeek(id) {
    setError("");
    try {
      const w = await api.getWeek(id);
      setPlan(w);
      setDayIdx(0);
      onSelect && onSelect(id);
    } catch (e) {
      setError(e.message);
    }
  }

  // Open externally-selected week (e.g. right after planning from the calendar).
  useEffect(() => {
    if (selectedId && (!plan || plan.id !== selectedId)) {
      openWeek(selectedId);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId]);

  const day = plan?.data?.days?.[dayIdx];
  const shiftsDef = plan?.data?.shifts_def;

  const activeWorkers = useMemo(() => {
    if (!day || !shiftsDef) return [];
    return day.workers.filter((w) => activeAt(shiftsDef[w.shift], hour));
  }, [day, shiftsDef, hour]);

  const blockColors = useMemo(() => {
    const m = {};
    for (const w of activeWorkers) {
      for (const b of w.blocks) m[b] = w.color;
    }
    return m;
  }, [activeWorkers]);

  // Map each active worker's zone id to their colour, so StreetMap can tint the
  // roads belonging to that zone — making road ownership visible even where a
  // street borders another worker's area.
  const zoneColor = useMemo(() => {
    const m = {};
    for (const w of activeWorkers) m[w.zone] = w.color;
    return m;
  }, [activeWorkers]);

  async function replan() {
    if (!plan) return;
    setBusy(true);
    setError("");
    try {
      const w = await api.replanWeek(plan.id);
      setPlan(w);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!plan) return;
    const ok = window.confirm(
      `¿Eliminar la semana del ${plan.week_start}?\n\n` +
        "Se borrará todo el plan de esa semana: las zonas, los turnos y las asignaciones " +
        "de cada trabajador. La semana volverá a figurar como no planificada en el Calendario " +
        "y dejará de aparecer en la Agenda de los trabajadores.\n\n" +
        "Esta acción no se puede deshacer."
    );
    if (!ok) return;
    setBusy(true);
    setError("");
    try {
      await api.deleteWeek(plan.id);
      setPlan(null);
      onSelect && onSelect(null);
      await loadWeeks();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="planview">
      <div className="panel sidebar">
        <h2>Semanas planificadas</h2>
        {weeks.length === 0 && <p className="muted">No hay semanas. Creá una en el Calendario.</p>}
        <ul className="list">
          {weeks.map((w) => (
            <li key={w.id} className={plan && plan.id === w.id ? "sel" : ""}>
              <button className="link" onClick={() => openWeek(w.id)}>
                Semana del {w.week_start}
              </button>
            </li>
          ))}
        </ul>
        {error && <div className="error">{error}</div>}
      </div>

      <div className="panel mapwrap">
        {!plan ? (
          <p className="muted">Elegí una semana para ver el plan en el mapa.</p>
        ) : (
          <>
            <div className="plan-controls">
              <div className="days">
                {plan.data.days.map((d, i) => (
                  <button
                    key={d.date}
                    className={"chip" + (i === dayIdx ? " active" : "")}
                    onClick={() => setDayIdx(i)}
                  >
                    {d.weekday}
                  </button>
                ))}
              </div>
              <div className="actions">
                <button onClick={replan} disabled={busy}>
                  Replanificar
                </button>
                <button
                  className="ghost"
                  disabled={busy}
                  onClick={async () => {
                    setBusy(true);
                    setError("");
                    try {
                      await downloadWeek(map, plan.data);
                    } catch (e) {
                      setError(e.message);
                    } finally {
                      setBusy(false);
                    }
                  }}
                  title="Descarga un PDF con un plano por trabajador y por día (blanco y negro, listo para imprimir)"
                >
                  Descargar semana (PDF)
                </button>
                <button className="danger" onClick={remove} disabled={busy}>
                  Eliminar
                </button>
              </div>
            </div>

            <div className="slider">
              <label>
                Hora del día: <strong>{fmtHour(hour)}</strong>
              </label>
              <input
                type="range"
                min={8}
                max={20}
                step={0.5}
                value={hour}
                onChange={(e) => setHour(parseFloat(e.target.value))}
              />
              <div className="ticks">
                <span>08:00</span>
                <span>14:00</span>
                <span>20:00</span>
              </div>
            </div>

            <div className="legend">
              {day.workers.map((w) => {
                const on = activeWorkers.includes(w);
                return (
                  <div key={w.worker_id + w.shift} className={"leg" + (on ? "" : " off")}>
                    <span className="sw" style={{ background: w.color }} />
                    <span className="nm">{w.worker_name}</span>
                    <span className="sh">{w.shift_label}</span>
                    <span className="st">{on ? "en calle" : "fuera"}</span>
                    <button
                      className="mini"
                      title={`Descargar plano B/N (JPG) de ${w.worker_name} (${day.weekday})`}
                      onClick={() =>
                        downloadWorkerDay(map, plan.data, day, w).catch((e) => setError(e.message))
                      }
                    >
                      ⬇ JPG
                    </button>
                  </div>
                );
              })}
            </div>

            <div className="map-area">
              <StreetMap
                map={map}
                blockColors={blockColors}
                roads={day.roads}
                zoneColor={zoneColor}
              />
            </div>
          </>
        )}
      </div>
    </div>
  );
}
