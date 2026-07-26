import React, { useEffect, useState } from "react";
import { api } from "./api";
import Workers from "./Workers";
import Calendar from "./Calendar";
import PlanView from "./PlanView";
import Schedule from "./Schedule";
import Settings from "./Settings";

const TABS = [
  { id: "workers", label: "Trabajadores" },
  { id: "calendar", label: "Calendario" },
  { id: "map", label: "Mapa" },
  { id: "schedule", label: "Agenda" },
  { id: "settings", label: "Ajustes" },
];

export default function App() {
  const [tab, setTab] = useState("workers");
  const [map, setMap] = useState(null);
  const [error, setError] = useState("");
  const [selectedWeek, setSelectedWeek] = useState(null);

  useEffect(() => {
    api
      .getMap()
      .then(setMap)
      .catch((e) => setError(e.message));
  }, []);

  return (
    <div className="app">
      <header>
        <h1>Grid Planner</h1>
        <span className="sub">{map?.label || ""}</span>
        <nav>
          {TABS.map((t) => (
            <button
              key={t.id}
              className={"tab" + (tab === t.id ? " active" : "")}
              onClick={() => setTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </nav>
      </header>

      <main>
        {error && <div className="error">Error cargando el mapa: {error}</div>}
        {tab === "workers" && <Workers />}
        {tab === "calendar" && (
          <Calendar
            onPlanned={(id) => {
              setSelectedWeek(id);
              setTab("map");
            }}
          />
        )}
        {tab === "map" &&
          (map ? (
            <PlanView map={map} selectedId={selectedWeek} onSelect={setSelectedWeek} />
          ) : (
            <div className="panel">
              <p className="muted">Cargando mapa…</p>
            </div>
          ))}
        {tab === "schedule" && <Schedule map={map} />}
        {tab === "settings" && <Settings />}
      </main>
    </div>
  );
}
