import React, { useEffect, useMemo, useState } from "react";
import { api } from "./api";
import { downloadWorkerWeek } from "./mapExport";

export default function Schedule({ map }) {
  const [workers, setWorkers] = useState([]);
  const [workerId, setWorkerId] = useState("");
  const [sched, setSched] = useState(null);
  const [weekIdx, setWeekIdx] = useState(0);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api.listWorkers().then(setWorkers).catch((e) => setError(e.message));
  }, []);

  useEffect(() => {
    if (!workerId) {
      setSched(null);
      return;
    }
    setError("");
    api
      .workerSchedule(workerId)
      .then((s) => {
        setSched(s);
        setWeekIdx(0);
      })
      .catch((e) => setError(e.message));
  }, [workerId]);

  const week = sched?.weeks?.[weekIdx];

  const total = useMemo(() => {
    if (!sched) return null;
    const worked = sched.weeks.reduce((a, w) => a + w.days_worked, 0);
    return { weeks: sched.weeks.length, worked };
  }, [sched]);

  async function downloadWeekPdf() {
    if (!week || !map) return;
    setBusy(true);
    setError("");
    try {
      const full = await api.getWeek(week.plan_id);
      await downloadWorkerWeek(map, full.data, Number(workerId), sched.worker.full_name);
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="panel">
      <h2>Agenda del trabajador</h2>
      <p className="muted">
        Elegí un trabajador para ver en qué días y turnos trabaja, según las semanas planificadas.
      </p>

      <div className="row">
        <select
          className="select"
          value={workerId}
          onChange={(e) => setWorkerId(e.target.value)}
        >
          <option value="">— Elegí un trabajador —</option>
          {workers.map((w) => (
            <option key={w.id} value={w.id}>
              {w.full_name}
            </option>
          ))}
        </select>

        {sched && sched.weeks.length > 0 && (
          <select
            className="select"
            value={weekIdx}
            onChange={(e) => setWeekIdx(Number(e.target.value))}
          >
            {sched.weeks.map((w, i) => (
              <option key={w.plan_id} value={i}>
                Semana del {w.week_start} ({w.days_worked}/7 días)
              </option>
            ))}
          </select>
        )}
      </div>

      {error && <div className="error">{error}</div>}

      {sched && sched.weeks.length === 0 && (
        <p className="muted">No hay semanas planificadas todavía. Creá una en el Calendario.</p>
      )}

      {total && (
        <p className="count">
          {sched.worker.full_name} aparece en <strong>{total.worked}</strong> turnos a lo largo de{" "}
          <strong>{total.weeks}</strong> {total.weeks === 1 ? "semana" : "semanas"}.
        </p>
      )}

      {week && (
        <>
          <div className="sched-actions">
            <button className="ghost" onClick={downloadWeekPdf} disabled={busy}>
              Descargar semana (PDF)
            </button>
          </div>
          <table className="sched">
            <thead>
              <tr>
                <th>Día</th>
                <th>Fecha</th>
                <th>Turno</th>
                <th>Horario</th>
              </tr>
            </thead>
            <tbody>
              {week.days.map((d) => {
                const a = d.assignment;
                return (
                  <tr key={d.date} className={a ? "on" : "off"}>
                    <td>{d.weekday}</td>
                    <td>{d.date}</td>
                    <td>
                      {a ? (
                        <span className="pill">
                          <span className="dot" style={{ background: a.color }} />
                          Turno {a.shift}
                        </span>
                      ) : (
                        <span className="franco">Franco</span>
                      )}
                    </td>
                    <td>{a ? a.shift_label : "—"}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </>
      )}
    </div>
  );
}
