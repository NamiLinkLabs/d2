#!/bin/sh
# Checks the @d2lang/d2 tarball that d2js/js/ci/build.sh packed and copies it
# to <output dir>/d2js-<release version>.tgz, the name it has on a release.
#
#   ci/release/d2js-package.sh <pack dir> <release version> <expected D2 version> <output dir>
#
# The package must report the expected D2 version (the release tag, or the
# short commit of an untagged build) and the version in d2js/js/package.json.
set -eu

if [ $# -ne 4 ]; then
  echo "usage: $0 <pack dir> <release version> <expected D2 version> <output dir>" >&2
  exit 2
fi
PACK_DIR=$1
RELEASE_VERSION=$2
D2_VERSION=$3
OUTPUT_DIR=$4
cd -- "$(dirname "$0")/../.."
NPM_VERSION=$(node -p "require('./d2js/js/package.json').version")

TARBALL=$(node - "$PACK_DIR/manifest.json" "$NPM_VERSION" <<'NODE'
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const [manifestPath, version] = process.argv.slice(2);
const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf8'));
const record = manifest.packages && manifest.packages['@d2lang/d2'];
if (manifest.schemaVersion !== 1 || manifest.version !== version || !record ||
    record.filename !== path.basename(record.filename)) {
  throw new Error('invalid npm package manifest');
}
const tarball = path.join(path.dirname(manifestPath), record.filename);
const bytes = fs.readFileSync(tarball);
const shasum = crypto.createHash('sha1').update(bytes).digest('hex');
const integrity = `sha512-${crypto.createHash('sha512').update(bytes).digest('base64')}`;
if (shasum !== record.shasum || integrity !== record.integrity) {
  throw new Error('npm tarball integrity mismatch');
}
process.stdout.write(tarball);
NODE
)

tar -xOzf "$TARBALL" package/package.json | node -e '
  let input = "";
  process.stdin.on("data", (chunk) => { input += chunk; });
  process.stdin.on("end", () => {
    const pkg = JSON.parse(input);
    if (pkg.name !== "@d2lang/d2" || pkg.version !== process.argv[1]) {
      throw new Error(`packed ${pkg.name}@${pkg.version}, expected @d2lang/d2@${process.argv[1]}`);
    }
  });
' "$NPM_VERSION"

CONSUMER=$(mktemp -d "${TMPDIR:-/tmp}/d2js-package.XXXXXX")
trap 'rm -rf "$CONSUMER"' EXIT HUP INT TERM
printf '%s\n' '{"private":true,"type":"module"}' >"$CONSUMER/package.json"
(cd "$CONSUMER" && npm install --ignore-scripts --no-audit --no-fund --package-lock=false "$TARBALL" >/dev/null)
cat >"$CONSUMER/smoke.js" <<'NODE'
import { D2 } from "@d2lang/d2";

const [expectedD2, expectedJS] = process.argv.slice(2);
const d2 = new D2();
try {
  const d2Version = await d2.version();
  const jsVersion = await d2.jsVersion();
  if (d2Version !== expectedD2) throw new Error(`version() is ${d2Version}, expected ${expectedD2}`);
  if (jsVersion !== expectedJS) throw new Error(`jsVersion() is ${jsVersion}, expected ${expectedJS}`);
  for (const layout of ["dagre", "elk", "tala"]) {
    const result = await d2.compile("a -> b\nb -> c: {source-port: right}\n", { layout });
    if (result.diagram.shapes.length !== 3) throw new Error(`${layout}: wrong shape count`);
    const svg = await d2.render(result.diagram, result.renderOptions);
    if (!svg.includes("<svg")) throw new Error(`${layout}: render did not produce SVG`);
  }
  console.log(`@d2lang/d2@${jsVersion} reports D2 ${d2Version}`);
} finally {
  await d2.dispose();
}
NODE
(cd "$CONSUMER" && node smoke.js "$D2_VERSION" "$NPM_VERSION")

mkdir -p "$OUTPUT_DIR"
cp "$TARBALL" "$OUTPUT_DIR/d2js-$RELEASE_VERSION.tgz"
ls -l "$OUTPUT_DIR/d2js-$RELEASE_VERSION.tgz"
