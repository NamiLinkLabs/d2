import { expect, test, describe } from "bun:test";
import { D2 } from "../../dist/node-esm/index.js";

const source = `
a: {style.fill: AA2}
users: {
  shape: sql_table
  id: int
  style.stroke: B4
  style.font-color: N7
}
a -> users: {style.stroke: AB4}
`;

describe("theme color codes", () => {
  test("compile keeps theme color codes and marks them explicit", async () => {
    const d2 = new D2();
    try {
      const result = await d2.compile(source);
      const shapes = Object.fromEntries(result.diagram.shapes.map((s) => [s.id, s]));
      expect(shapes.a.fill).toBe("AA2");
      expect(shapes.a.explicitThemeFill).toBe(true);
      expect(shapes.users.color).toBe("N7");
      expect(shapes.users.explicitThemeFontColor).toBe(true);
      expect(result.diagram.connections[0].stroke).toBe("AB4");
      expect(result.diagram.connections[0].explicitThemeStroke).toBe(true);
    } finally {
      await d2.dispose();
    }
  }, 20000);

  test("render follows the theme and the dark theme", async () => {
    const d2 = new D2();
    try {
      const result = await d2.compile(source);
      const svg = await d2.render(result.diagram, { themeID: 0, darkThemeID: 200 });
      expect(svg).toContain('class=" stroke-B1 fill-AA2"');
      expect(svg).toContain('class="connection stroke-AB4"');
      // The table header keeps its explicit N7 text rather than the B4 stroke.
      expect(svg).toContain('class="text fill-N7"');
      expect(svg).toContain(".fill-AA2{fill:#4A6FF3;}");
      expect(svg).toContain("prefers-color-scheme:dark");
      expect(svg).toContain(".fill-AA2{fill:#f38BA8;}");
    } finally {
      await d2.dispose();
    }
  }, 20000);

  test("invalid theme color codes report the valid codes", async () => {
    const d2 = new D2();
    try {
      let caught;
      try {
        await d2.compile("a.style.fill: b2");
      } catch (err) {
        caught = err;
      }
      expect(caught).toBeDefined();
      // Compile errors arrive as a JSON list of ranged messages.
      const [{ errmsg }] = JSON.parse(caught.message);
      expect(errmsg).toContain('a theme color code ("B2")');
      expect(errmsg).toContain("theme color codes are N1-N7, B1-B6, AA2, AA4, AA5, AB4 and AB5");
    } finally {
      await d2.dispose();
    }
  }, 20000);
});
