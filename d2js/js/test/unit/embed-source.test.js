import { expect, test, describe } from "bun:test";
import { D2 } from "../../dist/node-esm/index.js";

// A 1x1 PNG.
const png = Uint8Array.from(
  atob("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="),
  (c) => c.charCodeAt(0)
);
const withoutSource = (svg) => svg.replace(/<metadata><d2-source [\s\S]*?<\/metadata>/, "");

describe("embedded source", () => {
  test("compile with embedSource embeds the original files", async () => {
    const d2 = new D2();
    try {
      const fs = {
        index: "# kept\n...@lib/style\nx->y {class: hot}\n",
        "lib/style.d2": "classes: {hot: {style.fill: red}}\n",
      };
      const result = await d2.compile({ fs }, { layout: "elk", sketch: true, embedSource: true });
      expect(result.renderOptions.source.entry).toEqual({ path: "index", content: fs.index });
      expect(result.renderOptions.source.imports).toEqual([{ path: "lib/style.d2", content: fs["lib/style.d2"] }]);
      expect(result.renderOptions.source.settings).toEqual({ layout: "elk", sketch: true });

      const svg = await d2.render(result.diagram, result.renderOptions);
      expect(svg).toContain('<metadata><d2-source xmlns="https://d2lang.com/ns/source" format="1" path="index"');
      expect(svg).toContain("# kept\n...@lib/style\nx->y {class: hot}\n</d2-source>");

      const extracted = await d2.extractSource(svg);
      expect(extracted.inputPath).toBe("index");
      expect(extracted.fs).toEqual(fs);
      expect(extracted.options).toMatchObject({ layout: "elk", sketch: true, embedSource: true });
      const again = await d2.compile(extracted);
      const svg2 = await d2.render(again.diagram, again.renderOptions);
      expect(svg2).toBe(svg);
      // Bytes work too.
      expect((await d2.extractSource(new TextEncoder().encode(svg))).fs).toEqual(fs);
    } finally {
      await d2.dispose();
    }
  }, 60000);

  test("without embedSource nothing changes", async () => {
    const d2 = new D2();
    try {
      const result = await d2.compile("x -> y");
      expect("source" in result.renderOptions).toBe(false);
      const svg = await d2.render(result.diagram, result.renderOptions);
      expect(svg).not.toContain("<metadata>");
      expect(await d2.extractSource(svg)).toBeNull();
    } finally {
      await d2.dispose();
    }
  }, 60000);

  test("embedSource adds the source to PNG and SVG images", async () => {
    const d2 = new D2();
    try {
      const result = await d2.compile("a -> b", { themeID: 200, embedSource: true });
      const out = await d2.embedSource(png, result.renderOptions.source);
      expect(out).toBeInstanceOf(Uint8Array);
      expect(Array.from(out.subarray(0, 8))).toEqual(Array.from(png.subarray(0, 8)));
      const extracted = await d2.extractSource(out);
      expect(extracted.fs).toEqual({ index: "a -> b" });
      expect(extracted.options.themeID).toBe(200);
      expect(await d2.extractSource(png)).toBeNull();

      const plain = await d2.render(result.diagram, { ...result.renderOptions, source: undefined });
      const svg = await d2.embedSource(plain, result.renderOptions.source);
      expect(typeof svg).toBe("string");
      expect(withoutSource(svg)).toBe(plain);
      expect((await d2.extractSource(svg)).fs).toEqual({ index: "a -> b" });
      let caught;
      try {
        await d2.embedSource("not an image", result.renderOptions.source);
      } catch (err) {
        caught = err;
      }
      expect(caught?.message).toContain("not an SVG document");
    } finally {
      await d2.dispose();
    }
  }, 60000);
});
