import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { D2 } from "../../dist/node-esm/index.js";

// Whether segment a-b passes through the inside of box (Liang-Barsky).
function crosses(a, b, box, margin = 1) {
  const x0 = box.x + margin;
  const y0 = box.y + margin;
  const x1 = box.x + box.width - margin;
  const y1 = box.y + box.height - margin;
  const dx = b.x - a.x;
  const dy = b.y - a.y;
  let t0 = 0;
  let t1 = 1;
  for (const [p, q] of [
    [-dx, a.x - x0],
    [dx, x1 - a.x],
    [-dy, a.y - y0],
    [dy, y1 - a.y],
  ]) {
    if (p === 0) {
      if (q <= 0) return false;
      continue;
    }
    const r = q / p;
    if (p < 0) t0 = Math.max(t0, r);
    else t1 = Math.min(t1, r);
    if (t0 >= t1) return false;
  }
  return true;
}

const routeCrosses = (route, box) =>
  route.some((p, i) => i > 0 && crosses(route[i - 1], p, box));

const onOutline = (p, box, tolerance = 1.5) =>
  p.x >= box.x - tolerance &&
  p.x <= box.x + box.width + tolerance &&
  p.y >= box.y - tolerance &&
  p.y <= box.y + box.height + tolerance &&
  !(
    p.x > box.x + tolerance &&
    p.x < box.x + box.width - tolerance &&
    p.y > box.y + tolerance &&
    p.y < box.y + box.height - tolerance
  );

const toRouteShape = (s) => ({
  id: s.id,
  x: s.pos.x,
  y: s.pos.y,
  width: s.width,
  height: s.height,
  labelPosition: s.labelPosition,
  iconPosition: s.iconPosition,
});

describe("routeEdges request contract", () => {
  test("passes the request to the worker unchanged", async () => {
    const request = {
      fs: { index: "a -> b" },
      shapes: [{ id: "a", x: 0, y: 0, width: 10, height: 10 }],
      edges: ["(a -> b)[0]"],
      timeoutMs: 100,
    };
    const result = await D2.prototype.routeEdges.call(
      { sendMessage: async (type, data) => ({ type, data }) },
      request
    );
    expect(result).toEqual({ type: "routeEdges", data: request });
  });
});

describe("routeEdges", () => {
  let d2;
  beforeAll(() => {
    d2 = new D2();
  });
  afterAll(async () => {
    await d2.dispose();
  });

  test("routes a connection around a box moved into its way", async () => {
    const compiled = await d2.compile("a -> b\nb -> c\nd\n", { layout: "dagre" });
    const { diagram } = compiled;
    const shape = (id) => diagram.shapes.find((s) => s.id === id);
    const [a, b, d] = [shape("a"), shape("b"), shape("d")];
    // Drop d across the gap between a and b.
    d.pos = {
      x: Math.round(a.pos.x + a.width / 2 - 100),
      y: Math.round((a.pos.y + a.height + b.pos.y) / 2 - 15),
    };
    d.width = 200;
    d.height = 30;
    const dBox = { x: d.pos.x, y: d.pos.y, width: d.width, height: d.height };
    const ab = diagram.connections.find((c) => c.src === "a" && c.dst === "b");
    expect(routeCrosses(ab.route, dBox)).toBe(true);

    const result = await d2.routeEdges({
      fs: compiled.fs,
      options: { layout: "dagre" },
      shapes: diagram.shapes.map(toRouteShape),
      edges: [ab.id],
      connections: diagram.connections,
    });
    expect(result.errors).toBeUndefined();
    const route = result.routes[ab.id];
    expect(route.length).toBeGreaterThanOrEqual(2);
    expect(routeCrosses(route, dBox)).toBe(false);
    const box = (s) => ({ x: s.pos.x, y: s.pos.y, width: s.width, height: s.height });
    expect(onOutline(route[0], box(a))).toBe(true);
    expect(onOutline(route[route.length - 1], box(b))).toBe(true);

    ab.route = route;
    ab.isCurve = false;
    const svg = await d2.render(diagram, compiled.renderOptions);
    expect(svg).toContain("<svg");
  }, 30000);

  test("rejects invalid requests", async () => {
    const compiled = await d2.compile("a -> b", { layout: "dagre" });
    const shapes = compiled.diagram.shapes.map(toRouteShape);
    // Worker replies arrive as macrotasks, which bun's expect().rejects does
    // not wait for, so catch the errors directly.
    const failure = async (request) => {
      try {
        await d2.routeEdges(request);
      } catch (err) {
        return err.message;
      }
      return "no error";
    };
    expect(await failure({ fs: compiled.fs, shapes, edges: ["(a -> z)[0]"] })).toContain(
      'unknown edge id "(a -> z)[0]"'
    );
    expect(
      await failure({
        fs: compiled.fs,
        shapes: shapes.map((s) => ({ ...s, width: NaN })),
        edges: ["(a -> b)[0]"],
      })
    ).toContain("width must be a finite number");
    expect(
      await failure({ fs: compiled.fs, shapes: shapes.slice(1), edges: ["(a -> b)[0]"] })
    ).toContain("missing geometry for shape");
  }, 30000);
});
