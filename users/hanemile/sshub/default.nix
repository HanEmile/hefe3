{ pkgs ? import <nixpkgs> { } }:

pkgs.rustPlatform.buildRustPackage {
  pname = "sshub";
  version = "0.1.0";

  src = pkgs.lib.cleanSource ./.;

  cargoLock.lockFile = ./Cargo.lock;

  nativeBuildInputs = [ pkgs.makeWrapper ];

  # Make mosh-server discoverable on PATH so the SSH exec handler can spawn it.
  postFixup = ''
    wrapProgram $out/bin/sshub \
      --prefix PATH : ${pkgs.lib.makeBinPath [ pkgs.mosh ]}
  '';

  meta = with pkgs.lib; {
    description = "SSH-accessible TUI community hub with chat and forums (mosh-aware)";
    license = licenses.mit;
    mainProgram = "sshub";
  };
}
