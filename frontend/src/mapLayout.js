// Shared street-grid geometry, used by both the interactive StreetMap and the
// printable black-and-white export. Mirrors the backend map_config geometry +
// block indexing (index = row*cols + col).

export function buildLayout(map) {
  const G = map.geometry;
  const { BW, BH, SW, AVW, HALF, MT, ML, MR, MB, ANGLE } = G;
  const V = map.v_streets.map((s) => ({ ...s }));
  const H = map.h_streets.map((s) => ({ ...s }));

  let x = ML + HALF;
  V.forEach((s, i) => {
    s.w = s.av ? AVW : SW;
    s.x = x;
    s.cx = x + s.w / 2;
    x += s.w;
    if (i < V.length - 1) {
      s.colX = x;
      x += BW;
    }
  });
  const xEnd = x;

  let y = MT + HALF;
  H.forEach((s, i) => {
    s.w = s.av ? AVW : SW;
    s.y = y;
    s.cy = y + s.w / 2;
    y += s.w;
    if (i < H.length - 1) {
      s.rowY = y;
      y += BH;
    }
  });
  const yEnd = y;

  const coreLeft = V[0].x;
  const coreTop = H[0].y;
  const ringLeft = coreLeft - HALF;
  const ringRight = xEnd + HALF;
  const ringTop = coreTop - HALF;
  const ringBottom = yEnd + HALF;
  const totalW = xEnd + HALF + MR;
  const totalH = yEnd + HALF + MB;

  const cols = map.cols;
  const rows = map.rows;

  // Block rectangles indexed identically to the backend planner.
  const blocks = [];
  for (let r = 0; r < rows; r++) {
    for (let c = 0; c < cols; c++) {
      blocks[r * cols + c] = {
        x: V[c].colX,
        y: H[r].rowY,
        w: BW,
        h: BH,
      };
    }
  }

  const colCentre = (i) => V[i].colX + BW / 2;
  const rowCentre = (i) => H[i].rowY + BH / 2;
  const hLabelXs = [
    colCentre(0),
    colCentre(Math.floor((V.length - 1) / 2)),
    colCentre(V.length - 2),
  ];
  const vLabelYs = [rowCentre(0), rowCentre(H.length - 2)];

  // North-up rotation maths.
  const CX = (ringLeft + ringRight) / 2;
  const CY = (ringTop + ringBottom) / 2;
  const rad = (ANGLE * Math.PI) / 180;
  const fullW = ringRight - ringLeft;
  const fullH = ringBottom - ringTop;
  const rotW = fullW * Math.abs(Math.cos(rad)) + fullH * Math.abs(Math.sin(rad));
  const rotH = fullW * Math.abs(Math.sin(rad)) + fullH * Math.abs(Math.cos(rad));
  const SCALE = Math.min(totalW / rotW, totalH / rotH) * 0.95;

  return {
    V, H, BW, BH, HALF, ANGLE, cols, rows,
    xEnd, yEnd, ringLeft, ringRight, ringTop, ringBottom,
    coreLeft, coreTop, totalW, totalH,
    blocks, hLabelXs, vLabelYs,
    CX, CY, SCALE,
  };
}

// Geometry of a road segment from the backend ([type, r, c, zone]).
export function roadRect(L, t, r, c) {
  if (t === "v") return { x: L.V[c].x, y: L.H[r].rowY, w: L.V[c].w, h: L.BH };
  if (t === "h") return { x: L.V[c].colX, y: L.H[r].y, w: L.BW, h: L.H[r].w };
  return { x: L.V[c].x, y: L.H[r].y, w: L.V[c].w, h: L.H[r].w }; // "x" intersection
}
