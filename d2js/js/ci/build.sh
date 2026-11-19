#!/bin/sh
set -eu
. "$(dirname "$0")/../../../ci/sub/lib.sh"
. "$(dirname "$0")/npm-auth.sh"
cd -- "$(dirname "$0")/.."

cd ../..
if ! cmp -s ./LICENSE.txt ./d2js/js/LICENSE.txt; then
  echoerr "d2js/js/LICENSE.txt is out of sync with the repository license"
  exit 1
fi
if ! cmp -s ./THIRD_PARTY_NOTICES.txt ./d2js/js/THIRD_PARTY_NOTICES.txt; then
  echoerr "d2js/js/THIRD_PARTY_NOTICES.txt is out of sync with the repository notice"
  exit 1
fi
if [ -n "${NPM_VERSION:-}" ]; then
  JS_VERSION="$NPM_VERSION"
else
  JS_VERSION=$(awk -F'"' '/"version"/ {print $4}' ./d2js/js/package.json)
fi
# Match native builds: identify the exact release tag, or the source commit for
# an untagged development build. The npm package has its own independent version.
D2_VERSION=$(git_describe_ref)
sh_c "GOOS=js GOARCH=wasm go build -ldflags='-s -w -X github.com/d2lang/d2/lib/version.Version=${D2_VERSION} -X github.com/d2lang/d2/d2js/d2wasm.jsVersion=${JS_VERSION}' -trimpath -o main.wasm ./d2js"

if [ -n "${NPM_VERSION:-}" ]; then
  # Optimize with wasm-opt if available
  if command -v wasm-opt >/dev/null 2>&1; then
    echo "Optimizing WASM with wasm-opt..."
    # -Oz can use quadratic memory on large Go WASM modules.
    # https://github.com/WebAssembly/binaryen/issues/7644
    # Go 1.26 emits saturating float-to-integer conversions.
    sh_c "wasm-opt -O2 --enable-bulk-memory-opt --enable-nontrapping-float-to-int main.wasm -o main.wasm"
  else
    echo "wasm-opt not found, skipping optimization (install with: brew install binaryen)"
  fi
fi

sh_c "mv main.wasm ./d2js/js/wasm/d2.wasm"

if [ ! -f ./d2js/js/wasm/d2.wasm ]; then
  echoerr "Error: d2.wasm is missing"
  exit 1
else
  echo "d2.wasm exists. Size:"
  ls -lh ./d2js/js/wasm/d2.wasm | awk '{print $5}'
fi

cd d2js/js
if [ -n "${NPM_VERSION:-}" ]; then
  # Published packages always carry the compressed module.
  export D2JS_COMPRESS=1
fi
sh_c bun build.js

if [ -n "${NPM_VERSION:-}" ]; then
  PUBLISH_MODE="${NPM_PUBLISH_MODE:-publish}"
  # The fork publishes one package. Upstream's @d2lang/d2 and @terrastruct/d2
  # belong to the upstream project.
  PACKAGE_NAME="@namilinklabs/d2"
  PACKAGE_REGISTRY_PATH='%40namilinklabs%2Fd2'
  # Pack and publish with npm when it is installed, else with Bun, so a
  # direct publish works on a machine without npm. Staging needs npm.
  if command -v npm >/dev/null 2>&1; then
    PACKAGE_TOOL=npm
  else
    PACKAGE_TOOL=bun
  fi
  if [ "$PUBLISH_MODE" = stage ] && [ "$PACKAGE_TOOL" != npm ]; then
    echoerr "npm stage publish needs npm"
    exit 1
  fi

  if [ -n "${NPM_PACK_DIR:-}" ]; then
    case "$NPM_PACK_DIR" in
      /*) ;;
      *)
        echoerr "NPM_PACK_DIR must be an absolute path"
        exit 1
        ;;
    esac
    PACK_DIR=$NPM_PACK_DIR
    mkdir -p "$PACK_DIR"
    PRESERVE_PACK_DIR=1
  else
    PACK_DIR=$(mktemp -d "${TMPDIR:-/tmp}/d2-npm-publish.XXXXXX")
    PRESERVE_PACK_DIR=0
  fi

  restore_package_files() {
    remove_npm_auth_config
    if [ -f package.json.bak ]; then
      mv package.json.bak package.json
    fi
    if [ -f package-lock.json.bak ]; then
      mv package-lock.json.bak package-lock.json
    fi
  }

  cleanup_publish() {
    restore_package_files
    if [ "$PRESERVE_PACK_DIR" != 1 ]; then
      rm -f "$PACK_DIR"/*.json "$PACK_DIR"/*.tgz
      rmdir "$PACK_DIR" 2>/dev/null || true
    fi
  }
  trap cleanup_publish EXIT

  cp package.json package.json.bak
  if [ -f package-lock.json ]; then
    cp package-lock.json package-lock.json.bak
  fi

  CURRENT_NAME=$(node -p "require('./package.json').name")
  CURRENT_VERSION=$(node -p "require('./package.json').version")
  if [ "$CURRENT_NAME" != "$PACKAGE_NAME" ]; then
    echoerr "package.json must use the ${PACKAGE_NAME} name before publishing"
    exit 1
  fi
  if [ "$NPM_VERSION" = "nightly" ]; then
    echo "Publishing nightly version to npm..."

    DATE_TAG=$(date +'%Y%m%d')
    COMMIT_SHORT=$(git rev-parse --short HEAD)
    PUBLISH_VERSION="${CURRENT_VERSION}-nightly.${DATE_TAG}.${COMMIT_SHORT}"
    NPM_TAG="nightly"

    echo "Updating package version to ${PUBLISH_VERSION}"
  else
    echo "Publishing official version ${NPM_VERSION} to npm..."
    PUBLISH_VERSION="$NPM_VERSION"
    NPM_TAG="latest"

    echo "Setting package version to ${PUBLISH_VERSION}"
  fi

  # Stable staged releases must be versioned in source before dispatch so the
  # checked-in package state advances when the stages are approved.
  if { [ "$PUBLISH_MODE" = stage ] || [ "$PUBLISH_MODE" = pack ]; } &&
    [ "$NPM_VERSION" != nightly ] &&
    [ "$CURRENT_VERSION" != "$PUBLISH_VERSION" ]; then
    echoerr "Bump package.json and package-lock.json to ${PUBLISH_VERSION} before staging"
    exit 1
  fi

  # Sets the version in package.json and package-lock.json without npm.
  set_package_version() {
    node - "$1" <<'NODE'
const fs = require('fs');
const version = process.argv[2];
for (const filename of ['package.json', 'package-lock.json']) {
  if (!fs.existsSync(filename)) {
    continue;
  }
  const json = JSON.parse(fs.readFileSync(filename, 'utf8'));
  json.version = version;
  if (json.packages && json.packages['']) {
    json.packages[''].version = version;
  }
  fs.writeFileSync(filename, `${JSON.stringify(json, null, 2)}\n`);
}
NODE
  }

  if [ "$CURRENT_VERSION" != "$PUBLISH_VERSION" ]; then
    set_package_version "$PUBLISH_VERSION"
  fi
  if [ -f package-lock.json ]; then
    LOCK_NAME=$(node -p "require('./package-lock.json').name")
    LOCK_VERSION=$(node -p "require('./package-lock.json').version")
    LOCK_ROOT_NAME=$(node -p "require('./package-lock.json').packages[''].name")
    LOCK_ROOT_VERSION=$(node -p "require('./package-lock.json').packages[''].version")
    if [ "$LOCK_NAME" != "$PACKAGE_NAME" ] || [ "$LOCK_ROOT_NAME" != "$PACKAGE_NAME" ]; then
      echoerr "package-lock.json must use the ${PACKAGE_NAME} name before publishing"
      exit 1
    fi
    if [ "$LOCK_VERSION" != "$PUBLISH_VERSION" ] ||
      [ "$LOCK_ROOT_VERSION" != "$PUBLISH_VERSION" ]; then
      echoerr "package-lock.json versions do not match ${PUBLISH_VERSION}"
      exit 1
    fi
  fi

  if [ "$PACKAGE_TOOL" = npm ]; then
    pack_json=$(npm pack --json --pack-destination "$PACK_DIR")
    pack_metadata=$(printf '%s' "$pack_json" | node -e '
      let input = "";
      process.stdin.setEncoding("utf8");
      process.stdin.on("data", chunk => { input += chunk; });
      process.stdin.on("end", () => {
        const parsed = JSON.parse(input);
        const record = Array.isArray(parsed) ? parsed[0] : parsed[Object.keys(parsed)[0]];
        process.stdout.write([record.filename, record.shasum, record.integrity].join("\t"));
      });
    ')
  else
    pack_path=$(bun pm pack --quiet --destination "$PACK_DIR")
    pack_metadata=$(node - "$pack_path" <<'NODE'
const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const bytes = fs.readFileSync(process.argv[2]);
process.stdout.write([
  path.basename(process.argv[2]),
  crypto.createHash('sha1').update(bytes).digest('hex'),
  `sha512-${crypto.createHash('sha512').update(bytes).digest('base64')}`,
].join('\t'));
NODE
    )
  fi
  IFS="$(printf '\t')" read -r pack_filename PACKAGE_SHASUM PACKAGE_INTEGRITY <<EOF
$pack_metadata
EOF
  PACKAGE_TARBALL="$PACK_DIR/$pack_filename"
  PACKAGE_RESPONSE="$PACK_DIR/registry.json"
  if [ -z "$pack_filename" ] || [ -z "$PACKAGE_SHASUM" ] || [ -z "$PACKAGE_INTEGRITY" ] ||
    [ ! -f "$PACKAGE_TARBALL" ]; then
    echoerr "${PACKAGE_TOOL} pack did not return complete metadata for ${PACKAGE_NAME}"
    exit 1
  fi
  if ! tar -xOzf "$PACKAGE_TARBALL" package/LICENSE.txt | cmp - LICENSE.txt; then
    echoerr "${PACKAGE_NAME} package does not contain the repository license"
    exit 1
  fi
  if ! tar -xOzf "$PACKAGE_TARBALL" package/THIRD_PARTY_NOTICES.txt | cmp - THIRD_PARTY_NOTICES.txt; then
    echoerr "${PACKAGE_NAME} package does not contain the third-party notices"
    exit 1
  fi

  check_registry_version() {
    package_name=$1
    registry_path=$2
    expected_shasum=$3
    expected_integrity=$4
    response_file=$5

    if ! registry_http=$(curl -sS --retry 2 --retry-all-errors \
      -o "$response_file" \
      -w '%{http_code}' \
      "https://registry.npmjs.org/${registry_path}/${ENCODED_PUBLISH_VERSION}"); then
      echoerr "Unable to query npm registry for ${package_name}@${PUBLISH_VERSION}"
      return 1
    fi

    case "$registry_http" in
      404)
        REGISTRY_STATE=absent
        ;;
      200)
        registry_metadata=$(node - "$response_file" <<'NODE'
const fs = require('fs');
const metadata = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
process.stdout.write([
  metadata.dist && metadata.dist.shasum || '',
  metadata.dist && metadata.dist.integrity || '',
].join('\t'));
NODE
        )
        IFS="$(printf '\t')" read -r registry_shasum registry_integrity <<EOF
$registry_metadata
EOF
        if [ "$registry_shasum" = "$expected_shasum" ] &&
          [ "$registry_integrity" = "$expected_integrity" ]; then
          REGISTRY_STATE=matching
        else
          REGISTRY_STATE=mismatch
        fi
        ;;
      *)
        echoerr "npm registry returned HTTP ${registry_http} for ${package_name}@${PUBLISH_VERSION}"
        return 1
        ;;
    esac
  }

  ENCODED_PUBLISH_VERSION=$(node -p "encodeURIComponent(process.argv[1])" "$PUBLISH_VERSION")

  case "$PUBLISH_MODE" in
    pack)
      node - "$PACK_DIR/manifest.json" "$PUBLISH_VERSION" "$NPM_TAG" "$PACKAGE_NAME" \
        "${PACKAGE_TARBALL##*/}" "$PACKAGE_SHASUM" "$PACKAGE_INTEGRITY" <<'NODE'
const fs = require('fs');
const [manifestPath, version, tag, name, filename, shasum, integrity] = process.argv.slice(2);
const manifest = {
  schemaVersion: 1,
  version,
  tag,
  packages: {
    [name]: { filename, shasum, integrity },
  },
};
fs.writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
NODE
      echo "Packed ${PACKAGE_NAME} in $PACK_DIR"
      ;;
    stage)
      echo "Staging ${PACKAGE_NAME}@${PUBLISH_VERSION} with tag '${NPM_TAG}'..."
      npm stage publish "$PACKAGE_TARBALL" --tag "$NPM_TAG"
      echo "Successfully staged ${PACKAGE_NAME}@${PUBLISH_VERSION} with tag '${NPM_TAG}'"
      ;;
    publish)
      if [ -z "${NPM_TOKEN-}" ]; then
        echoerr "NPM_TOKEN environment variable is required for direct publishing"
        exit 1
      fi

      # Direct publishing is retained only for local bootstrap or recovery;
      # GitHub Actions uses trusted publishing. Keep its short-lived token out
      # of the checkout and in a private temporary npm user configuration.
      create_npm_auth_config

      # A retry may skip a version only when the registry tarball exactly
      # matches this run's pack.
      check_registry_version "$PACKAGE_NAME" "$PACKAGE_REGISTRY_PATH" \
        "$PACKAGE_SHASUM" "$PACKAGE_INTEGRITY" "$PACKAGE_RESPONSE"
      case "$REGISTRY_STATE" in
        matching)
          echo "Verified existing ${PACKAGE_NAME}@${PUBLISH_VERSION}; skipping publish."
          ;;
        mismatch)
          echoerr "Refusing to reuse ${PACKAGE_NAME}@${PUBLISH_VERSION}: registry integrity differs"
          exit 1
          ;;
        absent)
          echo "Publishing ${PACKAGE_NAME}@${PUBLISH_VERSION} with tag '${NPM_TAG}'..."
          if [ "$PACKAGE_TOOL" = npm ]; then
            NPM_CONFIG_USERCONFIG="$NPM_AUTH_CONFIG" npm publish "$PACKAGE_TARBALL" --tag "$NPM_TAG"
          else
            # Bun reads the token from NPM_CONFIG_TOKEN, only for this command.
            NPM_CONFIG_TOKEN=$(sed -n 's#^//registry.npmjs.org/:_authToken=##p' "$NPM_AUTH_CONFIG") \
              bun publish "$PACKAGE_TARBALL" --tag "$NPM_TAG" --access public
          fi

          published_verified=0
          for verify_attempt in 1 2 3 4 5; do
            if ! check_registry_version "$PACKAGE_NAME" "$PACKAGE_REGISTRY_PATH" \
              "$PACKAGE_SHASUM" "$PACKAGE_INTEGRITY" "$PACKAGE_RESPONSE"; then
              if [ "$verify_attempt" != 5 ]; then
                sleep 2
                continue
              fi
              break
            fi
            if [ "$REGISTRY_STATE" = matching ]; then
              published_verified=1
              break
            fi
            if [ "$REGISTRY_STATE" = mismatch ]; then
              break
            fi
            if [ "$verify_attempt" != 5 ]; then
              sleep 2
            fi
          done
          if [ "$published_verified" != 1 ]; then
            echoerr "Could not verify ${PACKAGE_NAME}@${PUBLISH_VERSION} after publishing"
            exit 1
          fi
          echo "Successfully published and verified ${PACKAGE_NAME}@${PUBLISH_VERSION} with tag '${NPM_TAG}'"
          ;;
      esac

      # For official direct releases, bump the checked-in canonical package
      # version after restoring the canonical manifests.
      if [ "$NPM_VERSION" != nightly ]; then
        restore_package_files
        CURRENT_VERSION=$(node -p "require('./package.json').version")
        if [ "$CURRENT_VERSION" != "$NPM_VERSION" ]; then
          set_package_version "$NPM_VERSION"
        fi
        git add package.json
        if [ -f package-lock.json ]; then
          git add package-lock.json
        fi
        if ! git diff --cached --quiet; then
          git commit -m "Bump version to ${NPM_VERSION} [skip ci]"
        fi
      fi
      ;;
    *)
      echoerr "Unknown NPM_PUBLISH_MODE: ${PUBLISH_MODE}"
      exit 1
      ;;
  esac
fi
