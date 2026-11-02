import { useEffect, useRef } from 'preact/hooks';

// Dodec is the a1 brand mark: a dodecahedron wireframe, projected from real
// 3D golden-ratio coordinates (not a hand-drawn path). `animate` spins it
// slowly on a 3-axis turntable — each frame re-projects the 20 vertices and
// repaints the 30 edges via direct attribute writes (no vdom churn), which
// costs ~300 setAttribute calls a frame and never touches the diff.
//
// prefers-reduced-motion pins it at the hero pose. The same geometry powers
// the favicon (scripts/gen_favicon.mjs keeps a static copy in sync).

const PHI = (1 + Math.sqrt(5)) / 2;

type Vec3 = [number, number, number];

// 20 vertices: (±1,±1,±1) plus the three cyclic permutations of
// (0, ±1/φ, ±φ). Edge length is exactly 2/φ.
function dodecVertices(): Vec3[] {
  const v: Vec3[] = [];
  for (const x of [-1, 1]) for (const y of [-1, 1]) for (const z of [-1, 1]) {
    v.push([x, y, z]);
  }
  const a = PHI, b = 1 / PHI;
  for (const s1 of [-1, 1]) for (const s2 of [-1, 1]) {
    v.push([0, s2 * b, s1 * a]);
    v.push([s2 * b, s1 * a, 0]);
    v.push([s1 * a, 0, s2 * b]);
  }
  return v;
}

const VERTS = dodecVertices();

// 30 edges: every pair at distance² == 4/φ² (floating-point tolerance for
// the irrational coordinates).
const EDGES: [number, number][] = (() => {
  const target = 4 / (PHI * PHI);
  const out: [number, number][] = [];
  for (let i = 0; i < VERTS.length; i++) {
    for (let j = i + 1; j < VERTS.length; j++) {
      const dx = VERTS[i][0] - VERTS[j][0];
      const dy = VERTS[i][1] - VERTS[j][1];
      const dz = VERTS[i][2] - VERTS[j][2];
      if (Math.abs(dx * dx + dy * dy + dz * dz - target) < 1e-9) {
        out.push([i, j]);
      }
    }
  }
  return out;
})();

// Hero pose: near the 3-fold axis through (1,1,1) — a corner-forward view
// with the silhouette reading as a nested hexagon/pentagon stack — offset a
// few degrees so it never looks like a flat diagram.
const BASE_RY = -0.58; // rad
const BASE_RX = 0.47;  // rad
const SPIN_RATE = 0.16; // rad/s around Y — one turn ≈ 39s
const WOBBLE = 0.12;    // rad of slow X breathing
const DIST = 9;         // camera distance (perspective strength)
const FOCAL = 118;

type Projected = { x: number; y: number; z: number };

function project(v: Vec3, ry: number, rx: number): Projected {
  const [x, y, z] = v;
  const cy = Math.cos(ry), sy = Math.sin(ry);
  const x1 = x * cy + z * sy;
  const z1 = -x * sy + z * cy;
  const cx = Math.cos(rx), sx = Math.sin(rx);
  const y2 = y * cx - z1 * sx;
  const z2 = y * sx + z1 * cx;
  const s = FOCAL / (DIST + z2);
  // SVG y grows downward; flip so the model's +y is up.
  return { x: x1 * s, y: -y2 * s, z: z2 };
}

const SEAL = '#537D96';
const SEAL_DEEP = '#3F6179';
const CINNABAR = '#8B2C1F';

interface Props {
  size?: number;
  animate?: boolean;
  class?: string;
}

export function Dodec({ size = 48, animate = true, class: cls }: Props) {
  const svgRef = useRef<SVGSVGElement>(null);

  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;

    const lines = Array.from(svg.querySelectorAll<SVGLineElement>('line.dodec-edge'));
    const dots = Array.from(svg.querySelectorAll<SVGCircleElement>('circle.dodec-vertex'));

    const paint = (t: number) => {
      const ry = BASE_RY + t * SPIN_RATE;
      const rx = BASE_RX + Math.sin(t * 0.21) * WOBBLE;
      const pts = VERTS.map(v => project(v, ry, rx));

      // Nearest vertex (max z) gets the cinnabar accent — its five edges
      // pop, which is what makes the wireframe read as a solid instead of
      // a tangle.
      let near = 0;
      for (let i = 1; i < pts.length; i++) {
        if (pts[i].z > pts[near].z) near = i;
      }
      const accent = new Set<number>();
      for (let e = 0; e < EDGES.length; e++) {
        if (EDGES[e][0] === near || EDGES[e][1] === near) accent.add(e);
      }

      for (let e = 0; e < EDGES.length; e++) {
        const [a, b] = EDGES[e];
        const line = lines[e];
        if (!line) continue;
        line.setAttribute('x1', pts[a].x.toFixed(2));
        line.setAttribute('y1', pts[a].y.toFixed(2));
        line.setAttribute('x2', pts[b].x.toFixed(2));
        line.setAttribute('y2', pts[b].y.toFixed(2));
        const depth = (pts[a].z + pts[b].z) / 2; // -1.7..1.7
        const norm = (depth + 1.8) / 3.6;        // 0..1
        line.setAttribute('stroke', accent.has(e) ? CINNABAR : norm > 0.55 ? SEAL : SEAL_DEEP);
        line.setAttribute('stroke-width', (0.7 + norm * 1.0).toFixed(2));
        line.setAttribute('stroke-opacity', (0.38 + norm * 0.62).toFixed(2));
      }
      for (let i = 0; i < pts.length; i++) {
        const dot = dots[i];
        if (!dot) continue;
        const norm = (pts[i].z + 1.8) / 3.6;
        dot.setAttribute('cx', pts[i].x.toFixed(2));
        dot.setAttribute('cy', pts[i].y.toFixed(2));
        dot.setAttribute('r', (i === near ? 1.9 : 0.6 + norm * 0.9).toFixed(2));
        dot.setAttribute('fill', i === near ? CINNABAR : SEAL);
        dot.setAttribute('fill-opacity', (0.25 + norm * 0.7).toFixed(2));
      }
    };

    if (!animate || window.matchMedia('(prefers-reduced-motion: reduce)').matches) {
      paint(0);
      return;
    }
    let raf = 0;
    const start = performance.now();
    const tick = (now: number) => {
      paint((now - start) / 1000);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [animate]);

  // Initial attributes are painted by the effect on mount; the values here
  // only matter for the first paint flash and SSR-ish cases.
  const initial = VERTS.map(v => project(v, BASE_RY, BASE_RX));

  return (
    <svg
      ref={svgRef}
      class={cls}
      width={size}
      height={size}
      viewBox="-32 -32 64 64"
      aria-hidden="true"
      style={{ filter: 'drop-shadow(0 1px 4px rgba(83, 125, 150, 0.35))' }}
    >
      {EDGES.map(([a, b], i) => (
        <line
          key={i}
          class="dodec-edge"
          x1={initial[a].x}
          y1={initial[a].y}
          x2={initial[b].x}
          y2={initial[b].y}
          stroke={SEAL}
          stroke-linecap="round"
        />
      ))}
      {VERTS.map((_, i) => (
        <circle key={i} class="dodec-vertex" cx={initial[i].x} cy={initial[i].y} r={1} />
      ))}
    </svg>
  );
}
