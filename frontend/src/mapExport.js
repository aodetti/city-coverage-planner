// Black-and-white printable map sheets, one per worker per day, exported as
// JPEG images or multi-page PDF.
//
// Each sheet shades the worker's assigned blocks in light grey, fills the
// streets they must patrol in solid black, and labels every street — so the
// sheet stays unambiguous when printed in greyscale (no colour is relied upon
// to convey assignment). Only patrolled streets carry a black line; uncovered
// streets are left blank to avoid confusion.

import { jsPDF } from "jspdf";
import { buildLayout, roadRect } from "./mapLayout";

const HEADER_H = 70; // space above the map for the title/meta text
const FOOTER_H = 30; // space below the map for the legend note

function esc(s) {
  return String(s).replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}

// Inner map markup (no <svg> wrapper); coordinates in the layout's own space.
function mapInner(L, day, worker) {
  const zone = worker.zone;
  const blockSet = new Set(worker.blocks);
  const { cols, rows } = L;
  const p = [];

  // Block grid: the worker's blocks shaded grey, the rest white, thin grid lines.
  L.blocks.forEach((b, idx) => {
    const mine = blockSet.has(idx);
    p.push(
      `<rect x="${b.x}" y="${b.y}" width="${b.w}" height="${b.h}" fill="${
        mine ? "#d7d7d7" : "#ffffff"
      }" stroke="#c8c8c8" stroke-width="1"/>`
    );
  });

  // Streets the worker must patrol, in solid black.
  for (const [t, r, c, z] of day.roads) {
    if (z !== zone) continue;
    const g = roadRect(L, t, r, c);
    p.push(`<rect x="${g.x}" y="${g.y}" width="${g.w}" height="${g.h}" fill="#111111"/>`);
  }

  // (No territory outline: the grey block shading already delimits the zone, and
  // an outline would draw black lines along uncovered boundary streets — which
  // reads as a patrol road and is confusing. Only patrolled streets are black.)

  // Street-name labels (white halo keeps them legible over grey/black).
  const label = (x, y, name, av, transform) =>
    `<text ${transform ? `transform="${transform}"` : `x="${x}" y="${y}"`} text-anchor="middle" ` +
    `dominant-baseline="middle" font-family="sans-serif" font-size="${av ? 12 : 11}" ` +
    `font-weight="bold" fill="#000000" paint-order="stroke" stroke="#ffffff" ` +
    `stroke-width="2.5" stroke-linejoin="round">${esc(name)}</text>`;

  L.H.forEach((s) => {
    L.hLabelXs.forEach((lx) => p.push(label(lx, s.cy, s.name, s.av)));
  });
  L.V.forEach((s) => {
    L.vLabelYs.forEach((ly) =>
      p.push(label(0, 0, s.name, s.av, `translate(${s.cx} ${ly}) rotate(-90)`))
    );
  });

  // Study-area border.
  p.push(
    `<rect x="${L.coreLeft}" y="${L.coreTop}" width="${L.xEnd - L.coreLeft}" height="${
      L.yEnd - L.coreTop
    }" fill="none" stroke="#888888" stroke-width="1.5"/>`
  );
  return p.join("");
}

// A complete, self-contained sheet SVG (title band + map + footer note).
function sheetSvg(L, weekStart, day, worker) {
  const W = L.totalW;
  const H = HEADER_H + L.totalH + FOOTER_H;
  const meta =
    `Día: ${day.weekday} ${day.date}    ` +
    `Turno: ${worker.shift_label}    ` +
    `Semana del: ${weekStart}`;
  return (
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}">` +
    `<rect x="0" y="0" width="${W}" height="${H}" fill="#ffffff"/>` +
    `<text x="20" y="34" font-family="sans-serif" font-size="26" font-weight="bold" fill="#000000">` +
    `${esc(worker.worker_name)}</text>` +
    `<text x="20" y="56" font-family="sans-serif" font-size="15" fill="#222222">${esc(meta)}</text>` +
    `<line x1="20" y1="${HEADER_H - 6}" x2="${W - 20}" y2="${HEADER_H - 6}" stroke="#000000" stroke-width="1"/>` +
    `<g transform="translate(0 ${HEADER_H})">${mapInner(L, day, worker)}</g>` +
    `<text x="20" y="${H - 10}" font-family="sans-serif" font-size="12" fill="#333333">` +
    `Zona asignada: bloques sombreados en gris. Calles a recorrer: en negro.</text>` +
    `</svg>`
  );
}

// Rasterise an SVG string to a JPEG data URL via an offscreen canvas.
function svgToJpeg(svgString, pxWidth) {
  return new Promise((resolve, reject) => {
    const vb = svgString.match(/viewBox="0 0 ([\d.]+) ([\d.]+)"/);
    const vw = parseFloat(vb[1]);
    const vh = parseFloat(vb[2]);
    const scale = pxWidth / vw;
    const cw = Math.round(vw * scale);
    const ch = Math.round(vh * scale);

    const blob = new Blob([svgString], { type: "image/svg+xml;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const img = new Image();
    img.onload = () => {
      const canvas = document.createElement("canvas");
      canvas.width = cw;
      canvas.height = ch;
      const ctx = canvas.getContext("2d");
      ctx.fillStyle = "#ffffff";
      ctx.fillRect(0, 0, cw, ch);
      ctx.drawImage(img, 0, 0, cw, ch);
      URL.revokeObjectURL(url);
      resolve({ dataUrl: canvas.toDataURL("image/jpeg", 0.92), w: cw, h: ch });
    };
    img.onerror = () => {
      URL.revokeObjectURL(url);
      reject(new Error("No se pudo renderizar el plano."));
    };
    img.src = url;
  });
}

function clickDownload(filename, href) {
  const a = document.createElement("a");
  a.href = href;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

function safeName(s) {
  return String(s || "worker")
    .normalize("NFD")
    .replace(/[̀-ͯ]/g, "")
    .replace(/[^\w-]+/g, "_");
}

// Assemble a set of sheet SVGs into one landscape A4 PDF (one sheet per page).
async function sheetsToPdf(svgs, filename) {
  const pdf = new jsPDF({ orientation: "landscape", unit: "pt", format: "a4" });
  const pw = pdf.internal.pageSize.getWidth();
  const ph = pdf.internal.pageSize.getHeight();
  const margin = 24;
  for (let i = 0; i < svgs.length; i++) {
    const { dataUrl, w, h } = await svgToJpeg(svgs[i], 1500);
    if (i > 0) pdf.addPage("a4", "landscape");
    const scale = Math.min((pw - 2 * margin) / w, (ph - 2 * margin) / h);
    const dw = w * scale;
    const dh = h * scale;
    pdf.addImage(dataUrl, "JPEG", (pw - dw) / 2, (ph - dh) / 2, dw, dh);
  }
  pdf.save(filename);
}

// ── Public API ──────────────────────────────────────────────────────────

// One worker's sheet for one day, as a JPEG image.
export async function downloadWorkerDay(map, weekData, day, worker) {
  const L = buildLayout(map);
  const { dataUrl } = await svgToJpeg(sheetSvg(L, weekData.week_start, day, worker), 1600);
  clickDownload(`plano_${safeName(worker.worker_name)}_${day.date}.jpg`, dataUrl);
}

// The full week: one page per worker per day, as a multi-page PDF.
export async function downloadWeek(map, weekData) {
  const L = buildLayout(map);
  const svgs = [];
  for (const d of weekData.days) {
    for (const w of d.workers) svgs.push(sheetSvg(L, weekData.week_start, d, w));
  }
  await sheetsToPdf(svgs, `planos_semana_${weekData.week_start}.pdf`);
}

// A single worker's working days for the week, as a multi-page PDF.
export async function downloadWorkerWeek(map, weekData, workerId, name) {
  const L = buildLayout(map);
  const svgs = [];
  for (const d of weekData.days) {
    const w = d.workers.find((x) => x.worker_id === workerId);
    if (w) svgs.push(sheetSvg(L, weekData.week_start, d, w));
  }
  if (!svgs.length) throw new Error("Este trabajador no tiene turnos en esta semana.");
  await sheetsToPdf(svgs, `agenda_${safeName(name)}_${weekData.week_start}.pdf`);
}
