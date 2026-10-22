let currentPort;
let d2;

// Images cross the WASM boundary as base64 inside JSON.
function imageBytes(image) {
  if (typeof image === "string") return new TextEncoder().encode(image);
  if (image instanceof Uint8Array) return image;
  if (image instanceof ArrayBuffer) return new Uint8Array(image);
  if (ArrayBuffer.isView(image)) return new Uint8Array(image.buffer, image.byteOffset, image.byteLength);
  throw new Error("image must be a string, Uint8Array or ArrayBuffer");
}

function bytesToBase64(bytes) {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  }
  return btoa(binary);
}

function base64ToBytes(base64) {
  return Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
}

function loadScript(content) {
  const func = new Function(content);
  func.call(globalThis);
}

export function setupMessageHandler(isNode, port, initWasm) {
  currentPort = port;

  const handleMessage = async (e) => {
    const { id, type, data } = e;

    switch (type) {
      case "init":
        try {
          if (isNode) {
            loadScript(data.wasmExecContent);
          }
          d2 = await initWasm(data.wasm);
          currentPort.postMessage({ type: "ready" });
        } catch (err) {
          currentPort.postMessage({ type: "error", error: err.message });
        }
        break;

      case "compile":
        try {
          const result = await d2.compile(JSON.stringify(data));
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({ id, type: "result", data: response.data });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "render":
        try {
          const result = await d2.render(JSON.stringify(data));
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          const decoded = new TextDecoder().decode(
            Uint8Array.from(atob(response.data), (c) => c.charCodeAt(0))
          );
          currentPort.postMessage({ id, type: "result", data: decoded });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "routeEdges":
        try {
          const result = await d2.routeEdges(JSON.stringify(data));
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({ id, type: "result", data: response.data });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "extractSource":
        try {
          const result = await d2.extractSource(
            JSON.stringify({ data: bytesToBase64(imageBytes(data)) })
          );
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({ id, type: "result", data: response.data ?? null });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "embedSource":
        try {
          const result = await d2.embedSource(
            JSON.stringify({ data: bytesToBase64(imageBytes(data.image)), source: data.source })
          );
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          const bytes = base64ToBytes(response.data);
          currentPort.postMessage({
            id,
            type: "result",
            data: typeof data.image === "string" ? new TextDecoder().decode(bytes) : bytes,
          });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "encode":
        try {
          const result = d2.encode(data);
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({
            id,
            type: "result",
            data: response.data.result,
          });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "decode":
        try {
          const result = d2.decode(data);
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({
            id,
            type: "result",
            data: response.data.result,
          });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "version":
        try {
          const result = d2.version();
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({ id, type: "result", data: response.data });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;

      case "jsVersion":
        try {
          const result = d2.jsVersion();
          const response = JSON.parse(result);
          if (response.error) throw new Error(response.error.message);
          currentPort.postMessage({ id, type: "result", data: response.data });
        } catch (err) {
          currentPort.postMessage({ id, type: "error", error: err.message });
        }
        break;
    }
  };

  if (isNode) {
    port.on("message", handleMessage);
  } else {
    port.onmessage = (e) => handleMessage(e.data);
  }
}

async function initWasmBrowser(wasmBinary) {
  const go = new Go();
  const result = await WebAssembly.instantiate(wasmBinary, go.importObject);
  go.run(result.instance);
  return self.d2;
}

setupMessageHandler(false, self, initWasmBrowser);
