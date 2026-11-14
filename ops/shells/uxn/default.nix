{ hefe, pkgs, ... }:

pkgs.mkShell {
  packages = [
    pkgs.uxn # uxnasm, uxnemu, uxncli
    hefe.ops.pkgs.uxn2 # uxn2 emulator + drifblim wrapper
  ];

  shellHook = ''
    echo "uxn toolchain available (sources live in ./uxn/):"
    echo "  uxnasm uxn/hello.tal uxn/hello.rom   # assemble"
    echo "  uxn2 uxn/hello.rom                   # run (also: uxnemu, uxncli)"
  '';
}
