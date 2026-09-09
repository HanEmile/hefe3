{
  lib,
  stdenv,
  fetchFromGitHub,
  go,
  ncurses,
}:
let
  cpus = {
    "x86_64" = "amd64";
    "i686" = "386";
    "aarch64" = "arm64";
  };
  targetSystem = lib.systems.parse.mkSystemFromString stdenv.targetPlatform.system;
  targetOS = targetSystem.kernel.name;
  targetArch = cpus.${targetSystem.cpu.name};
  targetVMArch = cpus.${(lib.systems.parse.mkSystemFromString stdenv.hostPlatform.system).cpu.name};
in
stdenv.mkDerivation (finalAttrs: {
  pname = "syzkaller";
  version = "0-unstable-2026-07-10";

  src = fetchFromGitHub {
    owner = "google";
    repo = "syzkaller";
    rev = "d78ea535f456172a095a6d211ee0a2de1d049968";
    hash = "sha256-c/UFUS3ndDzd813/f9CnFhhP6sjqDHmCEcm/ruXGEzY=";
  };

  nativeBuildInputs = [
    go
    ncurses
  ];

  postPatch =
    let
      commitDate = lib.concatStringsSep "" (
        builtins.tail (lib.splitString "-" (lib.removePrefix "0-" finalAttrs.version))
      );
      dateString = "${commitDate}-133700";
    in
    ''
      substituteInPlace Makefile \
        --replace-fail '$(shell git rev-parse HEAD)' '${finalAttrs.src.rev}' \
        --replace-fail '$(shell git diff --shortstat)' "" \
        --replace-fail '$(shell git log -n 1 --format="%cd" --date=format:%Y%m%d-%H%M%S)' '${dateString}'
    '';

  configurePhase = ''
    runHook preConfigure

    export GOCACHE=$TMPDIR/go-cache
    export GOPATH="$TMPDIR/go"

    runHook postConfigure
  '';

  makeFlags = [
    "TARGETOS=${targetOS}"
    "TARGETVMARCH=${targetVMArch}"
    "TARGETARCH=${targetArch}"
  ];

  installPhase = ''
    runHook preInstall

    mkdir -p $out/bin
    # Copy all top-level binaries
    cp -v bin/syz-* $out/bin/ 2>/dev/null || true
    # Copy target-specific binaries (fuzzer, executor)
    if [ -d "bin/${targetOS}_${targetArch}" ]; then
      cp -v bin/${targetOS}_${targetArch}/* $out/bin/ 2>/dev/null || true
    fi
    # Also check for the common linux_amd64 dir
    if [ -d "bin/linux_amd64" ]; then
      cp -v bin/linux_amd64/* $out/bin/ 2>/dev/null || true
    fi

    runHook postInstall
  '';

  meta = {
    description = "Unsupervised, coverage-guided kernel fuzzer (latest)";
    homepage = "https://github.com/google/syzkaller";
    license = lib.licenses.asl20;
    platforms = lib.platforms.unix;
    mainProgram = "syz-manager";
  };
})
