import React, { useEffect, useState } from "react";
import { api } from "./api";

export default function Settings() {
  const [current, setCurrent] = useState(null);
  const [defaultPath, setDefaultPath] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [showManual, setShowManual] = useState(false);
  const [path, setPath] = useState("");

  async function load() {
    try {
      const s = await api.getSettings();
      setCurrent(s.data_path);
      setDefaultPath(s.default_data_path);
    } catch (e) {
      setError(e.message);
    }
  }
  useEffect(() => {
    load();
  }, []);

  function confirmAndSwitch(picked, mode) {
    const verb =
      mode === "new" ? "crear una base nueva vacía" : "usar la base existente";
    const ok = window.confirm(
      `Se va a ${verb} en:\n\n${picked}\n\n` +
        "La aplicación se recargará para usar esta base. Los datos de la base " +
        "actual no se copian automáticamente.\n\n¿Continuar?"
    );
    return ok;
  }

  // Open the native OS file browser, then switch to the chosen file.
  async function browse(mode) {
    setError("");
    setBusy(true);
    try {
      const { path: picked } = await api.browseDB(mode);
      if (!picked) {
        // User cancelled the dialog.
        setBusy(false);
        return;
      }
      if (!confirmAndSwitch(picked, mode)) {
        setBusy(false);
        return;
      }
      await api.setDB(picked, mode);
      window.location.reload();
    } catch (e) {
      // If no native dialog is available, offer the manual field instead.
      if (/manualmente/i.test(e.message)) setShowManual(true);
      setError(e.message);
      setBusy(false);
    }
  }

  // Fallback: apply a hand-typed path.
  async function applyManual(mode) {
    setError("");
    if (!path.trim()) {
      setError("Ingresá la ruta del archivo de base de datos.");
      return;
    }
    if (!confirmAndSwitch(path.trim(), mode)) return;
    setBusy(true);
    try {
      await api.setDB(path.trim(), mode);
      window.location.reload();
    } catch (e) {
      setError(e.message);
      setBusy(false);
    }
  }

  return (
    <div className="panel">
      <h2>Ajustes</h2>
      <p className="muted">
        Elegí dónde se guarda la base de datos. Podés crear una base nueva y
        vacía, o abrir una que ya exista (por ejemplo, un archivo compartido o
        una copia de respaldo). Se abrirá el explorador de archivos para que la
        elijas.
      </p>

      <div className="count">
        Base de datos actual: <code>{current || "…"}</code>
      </div>

      <div className="row" style={{ marginTop: 8 }}>
        <button type="button" disabled={busy} onClick={() => browse("open")}>
          Abrir base existente…
        </button>
        <button type="button" disabled={busy} onClick={() => browse("new")}>
          Crear base nueva…
        </button>
      </div>
      {busy && <div className="muted" style={{ marginTop: 8 }}>Esperando el explorador de archivos…</div>}

      {error && <div className="error">{error}</div>}

      <ul className="list" style={{ marginTop: 16 }}>
        <li className="muted">
          <span>
            <strong>Abrir base existente:</strong> elegís un archivo de base que
            ya existe.
          </span>
        </li>
        <li className="muted">
          <span>
            <strong>Crear base nueva:</strong> elegís dónde guardar un archivo
            nuevo y vacío.
          </span>
        </li>
      </ul>

      <div style={{ marginTop: 12 }}>
        <button
          type="button"
          className="link"
          onClick={() => setShowManual((v) => !v)}
        >
          {showManual ? "Ocultar opción avanzada" : "Escribir la ruta manualmente"}
        </button>
      </div>

      {showManual && (
        <div style={{ marginTop: 8 }}>
          <form className="row" onSubmit={(e) => e.preventDefault()}>
            <input
              type="text"
              placeholder="/ruta/a/tu/base/data.json"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              style={{ flex: 1, minWidth: 280 }}
            />
          </form>
          <div className="row" style={{ marginTop: 8 }}>
            <button type="button" disabled={busy} onClick={() => applyManual("open")}>
              Usar existente
            </button>
            <button type="button" disabled={busy} onClick={() => applyManual("new")}>
              Crear nueva
            </button>
            {defaultPath && (
              <button
                type="button"
                className="link"
                disabled={busy}
                onClick={() => setPath(defaultPath)}
              >
                Usar ubicación por defecto
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
