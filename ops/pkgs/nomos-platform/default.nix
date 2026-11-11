{ pkgs, ... }:

let
  inherit (pkgs) buildGoModule fetchzip lib;

  rev = "9c1528a5ba2514e8df60324c03b3cd2a9a0b6013"; # origin/feature/platform
in
buildGoModule rec {
  pname = "nomos-platform";
  version = "0.0.0-unstable-2026-11-08";

  src = fetchzip {
    url = "https://codeberg.org/nomoi/nomos/archive/${rev}.tar.gz";
    hash = "sha256-lh5vOABZvrHrV7lxC0JUIZJ6jr/DmSCuGH6VGcD4aZQ=";
  };

  vendorHash = "sha256-LEaPPrF2xVS5/GV9HnnY6sXJHuOsEx15nImKjEUZUP4=";

  # The branch carries a committed vendor/ directory; Nix vendors from go.sum
  # instead (the committed one disagrees with go.mod).
  postPatch = ''
    rm -rf vendor
  '';

  env.CGO_ENABLED = 0;

  subPackages = [ "cmd/nomos" ];

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
    "-X main.commit=${rev}"
  ];

  # The wallet's WebAssembly module and its wasm_exec.js are built, never
  # committed — the module records the toolchain that compiled it (see the
  # `wasm` target in the repo Makefile). The rest of the WebUI bundle is
  # committed under nomos/platform/webdist/ and embedded as-is.
  preBuild = ''
    mkdir -p .build
    GOOS=js GOARCH=wasm go build -trimpath -buildvcs=false -ldflags '-s -w' -o .build/nomoi.wasm ./cmd/nomos-wasm
    gzip -9 -n -c .build/nomoi.wasm > nomos/platform/webdist/nomoi.wasm.gz
    cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" nomos/platform/webdist/wasm_exec.js
  '';

  # The wasm step must not run in the vendor-only derivation: there, vendor/
  # is deliberately absent and GOFLAGS=-mod=vendor, so a `go build` would
  # mistake the missing tree for inconsistent vendoring and fail.
  overrideModAttrs = _: {
    preBuild = "";
  };

  # Unit suite only (e2e/integration stay behind their build tags; the python
  # operations/ suite drives docker compose and is a deploy-time concern).
  checkPhase = ''
    runHook preCheck
    # Tests resolve XDG paths under HOME; point it somewhere writable.
    # -skip: the supervisor tests re-execute the test binary as a managed
    # worker process and drive its full lifecycle, which does not survive the
    # nix build sandbox (upstream runs them via `make test`).
    export HOME=$TMPDIR
    go test -skip 'TestSupervisorIsolationCrashRecoveryAndRegistryGates|TestWorkerRefusesAnotherTAsConfiguration' ./...
    runHook postCheck
  '';

  meta = with lib; {
    description = "NOMOI administration appliance (nomos platform) — WebUI + management API for trust anchors";
    homepage = "https://codeberg.org/nomoi/nomos";
    license = licenses.asl20;
    mainProgram = "nomos";
  };
}
