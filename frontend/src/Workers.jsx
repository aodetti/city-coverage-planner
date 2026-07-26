import React, { useEffect, useState } from "react";
import { api } from "./api";

export default function Workers() {
  const [list, setList] = useState([]);
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function load() {
    try {
      setList(await api.listWorkers());
    } catch (e) {
      setError(e.message);
    }
  }
  useEffect(() => {
    load();
  }, []);

  async function add(e) {
    e.preventDefault();
    setError("");
    if (!name.trim()) {
      setError("Ingresá un nombre completo.");
      return;
    }
    setBusy(true);
    try {
      await api.createWorker(name.trim());
      setName("");
      await load();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }

  async function remove(worker) {
    const ok = window.confirm(
      `¿Eliminar al trabajador «${worker.full_name}»?\n\n` +
        "Se quitará de la lista y no podrá asignarse a nuevas planificaciones. " +
        "Las semanas ya planificadas no se modifican: conservan el plan tal como se generó.\n\n" +
        "Esta acción no se puede deshacer."
    );
    if (!ok) return;
    setError("");
    try {
      await api.deleteWorker(worker.id);
      await load();
    } catch (e) {
      setError(e.message);
    }
  }

  return (
    <div className="panel">
      <h2>Trabajadores</h2>
      <p className="muted">
        Registrá a los trabajadores. Se necesitan al menos 6 para planificar una semana.
      </p>

      <form onSubmit={add} className="row">
        <input
          type="text"
          placeholder="Nombre y apellido"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <button type="submit" disabled={busy}>
          Agregar
        </button>
      </form>
      {error && <div className="error">{error}</div>}

      <div className="count">
        {list.length} trabajador{list.length === 1 ? "" : "es"}
        {list.length < 6 && (
          <span className="warn"> — faltan {6 - list.length} para planificar</span>
        )}
      </div>

      <ul className="list">
        {list.map((w) => (
          <li key={w.id}>
            <span>{w.full_name}</span>
            <button className="link danger" onClick={() => remove(w)}>
              Eliminar
            </button>
          </li>
        ))}
        {list.length === 0 && <li className="muted">Sin trabajadores aún.</li>}
      </ul>
    </div>
  );
}
