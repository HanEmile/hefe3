{ hefe, pkgs, ... }:

pkgs.mkShell {
  packages = [
    pkgs.uxn # uxnasm, uxnemu, uxncli
    hefe.ops.pkgs.uxn2 # uxn2 emulator + drifblim wrapper
  ];

  shellHook = ''
    echo "uxn toolchain available:"
    echo "  uxnasm hello.tal hello.rom   # assemble"
    echo "  uxn2 hello.rom               # run (also: uxnemu, uxncli)"
  '';
}
