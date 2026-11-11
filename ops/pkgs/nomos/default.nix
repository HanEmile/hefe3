{ pkgs, ... }:

let
  inherit (pkgs) buildGoModule fetchzip lib;
in
buildGoModule rec {
  pname = "nomos";
  version = "0.0.0-unstable-2026-11-08";

  src = fetchzip {
    url = "https://codeberg.org/nomoi/nomos/archive/3cd9b536c07ab9c5965065257b5c59076172f108.tar.gz";
    hash = "sha256-HWCAU3EkXidGSpUpDeOQnUxwNfVVwiGQ5ZD+CbzjNXA=";
  };

  vendorHash = "sha256-av3UbCX546QX5jVxD39CYE7yItkqKN5UPCTDuaaxLwc=";

  env.CGO_ENABLED = 0;

  subPackages = [ "cmd/nomos" ];

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}"
    "-X main.commit=3cd9b53"
  ];

  # Run the full unit suite (e2e/integration stay behind their build tags).
  checkPhase = ''
    runHook preCheck
    # Tests resolve XDG paths under HOME; point it somewhere writable.
    export HOME=$TMPDIR
    go test ./...
    runHook postCheck
  '';

  meta = with lib; {
    description = "Go client and reference implementation of the NOMOI protocol";
    homepage = "https://codeberg.org/nomoi/nomos";
    license = licenses.asl20;
    mainProgram = "nomos";
  };
}
