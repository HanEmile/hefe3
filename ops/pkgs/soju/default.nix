# nixpkgs lags upstream soju releases; this tracks the latest tag directly
# so we can pick up new features (e.g. client-cert-auth, added after the
# 0.10.1 nixpkgs currently packages) without waiting for a channel bump.
{ ... }:
{
  lib,
  buildGoModule,
  fetchFromCodeberg,
  installShellFiles,
  scdoc,
}:

buildGoModule rec {
  pname = "soju";
  version = "0.11.1";

  src = fetchFromCodeberg {
    owner = "emersion";
    repo = "soju";
    rev = "v${version}";
    hash = "sha256-OoFAdHnExm6Li5bTAmS/KdRkkpHPLP321GAYYqByrNI=";
  };

  patches = [
    # Keep the internal nick-regain MONITOR watch alive when the last
    # MONITOR-aware downstream disconnects (not yet fixed upstream).
    ./retain-regain-nick-monitor.patch
  ];

  vendorHash = "sha256-LiTr+ilKYRA3K93RjVGsmWm2jvImmt9YIYTZPtXv6cE=";

  nativeBuildInputs = [
    installShellFiles
    scdoc
  ];

  ldflags = [
    "-s"
    "-w"
    "-X codeberg.org/emersion/soju/config.DefaultPath=/etc/soju/config"
    "-X codeberg.org/emersion/soju/config.DefaultUnixAdminPath=/run/soju/admin"
  ];

  postBuild = ''
    make doc/soju.1 doc/sojuctl.1
  '';

  checkFlags = [ "-skip TestPostgresMigrations" ];

  postInstall = ''
    installManPage doc/soju.1 doc/sojuctl.1
  '';

  meta = {
    description = "User-friendly IRC bouncer";
    homepage = "https://soju.im";
    changelog = "https://codeberg.org/emersion/soju/releases/tag/v${version}";
    license = lib.licenses.agpl3Only;
    mainProgram = "sojuctl";
  };
}
