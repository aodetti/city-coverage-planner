import React, { useMemo, useRef, useState, useEffect } from "react";
import { buildLayout, roadRect } from "./mapLayout";

// Opacity for the worker colour overlays on the *blocks*, kept fairly light so
// the soft-grey street grid beneath the coloured zones stays legible.
const OVERLAY_OPACITY = 0.5;
// Roads are tinted more strongly than blocks: drawn over the darker grey road
// bed, a higher opacity yields a richer, slightly darker shade of the owner's
// colour — so each street still reads as a street, but its owner is obvious
// even where it borders another worker's zone.
const ROAD_OPACITY = 0.72;

export default function StreetMap({ map, blockColors, roads, zoneColor }) {
  const L = useMemo(() => buildLayout(map), [map]);

  // Per-road rectangles tinted by their owning worker's colour. We only tint
  // roads whose owner is currently "en calle" (present in zoneColor); the rest
  // stay as the plain grey road bed.
  const roadFills = useMemo(() => {
    if (!roads || !zoneColor) return [];
    const out = [];
    for (const [t, r, c, zone] of roads) {
      const fill = zoneColor[zone];
      if (!fill) continue;
      out.push({ ...roadRect(L, t, r, c), fill, key: `${t}-${r}-${c}` });
    }
    return out;
  }, [roads, zoneColor, L]);

  const svgRef = useRef(null);
  const [vp, setVp] = useState({ x: 0, y: 0, k: 1 });
  const [northUp, setNorthUp] = useState(false);
  const drag = useRef(null);

  const MIN_K = 0.4, MAX_K = 8;

  function toSvg(evt) {
    const svg = svgRef.current;
    const pt = svg.createSVGPoint();
    pt.x = evt.clientX;
    pt.y = evt.clientY;
    return pt.matrixTransform(svg.getScreenCTM().inverse());
  }

  function onWheel(evt) {
    evt.preventDefault();
    const p = toSvg(evt);
    const factor = evt.deltaY < 0 ? 1.1 : 1 / 1.1;
    setVp((cur) => {
      const k = Math.min(MAX_K, Math.max(MIN_K, cur.k * factor));
      const ratio = k / cur.k;
      return {
        k,
        x: p.x - (p.x - cur.x) * ratio,
        y: p.y - (p.y - cur.y) * ratio,
      };
    });
  }

  // Wheel must be a non-passive native listener to allow preventDefault.
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;
    svg.addEventListener("wheel", onWheel, { passive: false });
    return () => svg.removeEventListener("wheel", onWheel);
  }, [L]);

  function onMouseDown(evt) {
    if (evt.target.closest("#compass")) return;
    const p = toSvg(evt);
    drag.current = { sx: p.x, sy: p.y, ox: vp.x, oy: vp.y };
  }
  function onMouseMove(evt) {
    if (!drag.current) return;
    const p = toSvg(evt);
    setVp((cur) => ({
      ...cur,
      x: drag.current.ox + (p.x - drag.current.sx),
      y: drag.current.oy + (p.y - drag.current.sy),
    }));
  }
  function onMouseUp() {
    drag.current = null;
  }

  const mapTransform = northUp
    ? `translate(${L.totalW / 2} ${L.totalH / 2}) scale(${L.SCALE.toFixed(
        4
      )}) rotate(${L.ANGLE}) translate(${-L.CX} ${-L.CY})`
    : undefined;
  const roseTransform = northUp ? "rotate(0)" : `rotate(${-L.ANGLE})`;

  const ringW = L.ringRight - L.ringLeft;
  const ringH = L.ringBottom - L.ringTop;

  return (
    <svg
      ref={svgRef}
      viewBox={`0 0 ${L.totalW} ${L.totalH}`}
      style={{ width: "100%", height: "100%", cursor: drag.current ? "grabbing" : "grab", touchAction: "none" }}
      onMouseDown={onMouseDown}
      onMouseMove={onMouseMove}
      onMouseUp={onMouseUp}
      onMouseLeave={onMouseUp}
    >
      <rect x={0} y={0} width={L.totalW} height={L.totalH} fill="#d6d0c4" />

      <g transform={`translate(${vp.x} ${vp.y}) scale(${vp.k})`}>
        <g transform={mapTransform}>
          {/* Road bed */}
          <rect className="road" x={L.ringLeft} y={L.ringTop} width={ringW} height={ringH} />

          {/* Ring half-blocks */}
          {L.V.slice(0, -1).map((s, c) => (
            <React.Fragment key={`ring-v-${c}`}>
              <rect className="block" x={s.colX} y={L.ringTop} width={L.BW} height={L.HALF} />
              <rect className="block" x={s.colX} y={L.yEnd} width={L.BW} height={L.HALF} />
            </React.Fragment>
          ))}
          {L.H.slice(0, -1).map((s, r) => (
            <React.Fragment key={`ring-h-${r}`}>
              <rect className="block" x={L.ringLeft} y={s.rowY} width={L.HALF} height={L.BH} />
              <rect className="block" x={L.xEnd} y={s.rowY} width={L.HALF} height={L.BH} />
            </React.Fragment>
          ))}
          {[
            [L.ringLeft, L.ringTop],
            [L.xEnd, L.ringTop],
            [L.ringLeft, L.yEnd],
            [L.xEnd, L.yEnd],
          ].map(([cx, cy], i) => (
            <rect key={`corner-${i}`} className="block" x={cx} y={cy} width={L.HALF} height={L.HALF} />
          ))}

          {/* Main grid blocks (with optional zone coloring) */}
          {L.blocks.map((b, idx) => {
            const fill = blockColors && blockColors[idx];
            return (
              <rect
                key={`b-${idx}`}
                className="block"
                x={b.x}
                y={b.y}
                width={b.w}
                height={b.h}
                style={fill ? { fill, fillOpacity: OVERLAY_OPACITY } : undefined}
              />
            );
          })}

          {/* Avenue shading */}
          {L.V.filter((s) => s.av).map((s, i) => (
            <rect key={`av-v-${i}`} className="avenue" x={s.x} y={L.ringTop} width={s.w} height={ringH} />
          ))}
          {L.H.filter((s) => s.av).map((s, i) => (
            <rect key={`av-h-${i}`} className="avenue" x={L.ringLeft} y={s.y} width={ringW} height={s.w} />
          ))}

          {/* Roads tinted by their owning worker (drawn over the grey bed) */}
          {roadFills.map((rf) => (
            <rect
              key={`rd-${rf.key}`}
              x={rf.x}
              y={rf.y}
              width={rf.w}
              height={rf.h}
              fill={rf.fill}
              fillOpacity={ROAD_OPACITY}
            />
          ))}

          {/* Study-area border */}
          <rect className="border" x={L.coreLeft} y={L.coreTop} width={L.xEnd - L.coreLeft} height={L.yEnd - L.coreTop} />

          {/* Labels */}
          {L.H.map((s, i) =>
            L.hLabelXs.map((lx, j) => (
              <text
                key={`hl-${i}-${j}`}
                className={s.av ? "av-label" : "street-label"}
                x={lx}
                y={s.cy}
                textAnchor="middle"
                dominantBaseline="middle"
              >
                {s.name}
              </text>
            ))
          )}
          {L.V.map((s, i) =>
            L.vLabelYs.map((ly, j) => (
              <text
                key={`vl-${i}-${j}`}
                className={s.av ? "av-label" : "street-label"}
                transform={`translate(${s.cx} ${ly}) rotate(-90)`}
                textAnchor="middle"
                dominantBaseline="middle"
              >
                {s.name}
              </text>
            ))
          )}
        </g>
      </g>

      {/* Compass (fixed, click toggles north-up) */}
      <g
        id="compass"
        transform={`translate(46 ${L.totalH - 28})`}
        style={{ cursor: "pointer" }}
        onClick={() => setNorthUp((v) => !v)}
      >
        <title>Click para alternar entre vista esquemática y orientación al Norte real</title>
        <circle cx={0} cy={0} r={22} fill="#fff" stroke="#555" strokeWidth={1.5} />
        <g id="compassRose" transform={roseTransform}>
          <text x={0} y={-25} textAnchor="middle" fontSize={11} fontWeight="bold" fill="#c0392b">N</text>
          <polygon points="0,-19 4,-6 0,-10 -4,-6" fill="#c0392b" />
          <polygon points="0,19 4,6 0,10 -4,6" fill="#888" />
        </g>
        <text className="hint" x={0} y={40} textAnchor="middle">girar</text>
      </g>
    </svg>
  );
}
