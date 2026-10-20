import {
  D2,
  type RouteEdgesRequest,
  type RouteEdgesResponse,
  type RoutePoint,
} from "../../index.js";

declare const d2: D2;

d2.compile("a -> b\nc", { layout: "dagre" }).then(async ({ diagram, fs }) => {
  const request: RouteEdgesRequest = {
    fs,
    options: { layout: "dagre" },
    shapes: diagram.shapes.map((s) => ({
      id: s.id,
      x: s.pos.x,
      y: s.pos.y,
      width: s.width,
      height: s.height,
      labelPosition: s.labelPosition,
      iconPosition: s.iconPosition,
    })),
    edges: diagram.connections.map((c) => c.id),
    // Compiled connections can be passed as they are.
    connections: diagram.connections,
    timeoutMs: 500,
  };
  const response: RouteEdgesResponse = await d2.routeEdges(request);
  for (const connection of diagram.connections) {
    const route: RoutePoint[] | undefined = response.routes[connection.id];
    if (!route) {
      const reason: string | undefined = response.errors?.[connection.id];
      void reason;
      continue;
    }
    connection.route = route;
    connection.isCurve = false;
    const label = response.labels?.[connection.id];
    if (label) {
      connection.labelPosition = label.position;
      connection.labelPercentage = label.percentage;
    }
  }
  await d2.render(diagram);
});

// @ts-expect-error The connections to route are required.
d2.routeEdges({ fs: { index: "a -> b" }, shapes: [] });
d2.routeEdges({
  fs: { index: "a" },
  // @ts-expect-error Shapes give their box as flat numbers.
  shapes: [{ id: "a", pos: { x: 0, y: 0 }, width: 1, height: 1 }],
  edges: [],
});
// @ts-expect-error Edge ids are strings.
d2.routeEdges({ fs: { index: "a -> b" }, shapes: [], edges: [0] });
