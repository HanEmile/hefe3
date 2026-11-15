{ pkgs, ... }:

let
  inherit (pkgs) buildGoModule lib;
in
buildGoModule {
  pname = "r2wars-web";
  version = "0.1.0";

  src = ./.;

  # vendor/ is committed in-tree (same as the go.mod/go.sum it was generated
  # from), so there's nothing to fetch.
  vendorHash = null;

  # r2pipe-go falls back to a native libr_core cgo binding unless CGO is
  # disabled, which this package doesn't vendor/link against - it talks to
  # r2/rasm2 over a pipe, spawning them from $PATH at runtime instead.
  env.CGO_ENABLED = "0";

  subPackages = [ "cmd/r2wars-web" ];

  # buildGoModule scopes its default checkPhase to subPackages too, which
  # would silently skip every test in internal/* (that's where they all
  # live - cmd/r2wars-web itself has none). Override it to cover the whole
  # module: no network or real radare2 binary needed, so this is a real,
  # cheap regression gate rather than a thing to disable for speed.
  checkPhase = ''
    runHook preCheck
    go test ./...
    runHook postCheck
  '';

  # Templates are read from disk at startup (-templates flag), not embedded,
  # so ship them next to the binary.
  postInstall = ''
    mkdir -p $out/share/r2wars-web
    cp -r templates $out/share/r2wars-web/templates
  '';

  meta = {
    description = "r2wars: a radare2-based core-war style bot arena (web frontend)";
    homepage = "https://github.com/hanemile/hefe3/tree/main/users/hanemile/projects/go/r2wars-web";
    license = lib.licenses.mit;
    maintainers = [ ];
    mainProgram = "r2wars-web";
  };
}
