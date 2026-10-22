import { describe, expect, test } from "bun:test";
import { D2 } from "../../dist/node-esm/index.js";

describe("label.padding", () => {
  test.each(["dagre", "elk", "tala"])(
    "reaches the diagram and the render with %s",
    async (layout) => {
      const d2 = new D2();
      try {
        const result = await d2.compile(
          "a -> b: padded {label.padding: 9; style.fill: red}\nc -> d: plain",
          { layout }
        );
        const [padded, plain] = result.diagram.connections;
        expect(padded.labelPadding).toBe(9);
        expect(plain.labelPadding).toBeUndefined();
        // labelWidth and labelHeight stay the size of the text.
        expect(padded.labelHeight).toBe(plain.labelHeight);

        const svg = await d2.render(result.diagram);
        const width = padded.labelWidth + 18;
        const height = padded.labelHeight + 18;
        // The fill covers the padded box.
        expect(svg).toContain(`width="${width}" height="${height}" rx="10" fill="red"`);
      } finally {
        await d2.dispose();
      }
    },
    20000
  );
});
