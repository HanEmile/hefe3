{
  lib,
  stdenv,
  fetchFromGitHub,
  meson,
  ninja,
  pkg-config,
  python3,
  capstone,
  file,
  libewf,
  libusb-compat-0_1,
  libuv,
  libzip,
  lz4,
  openssl,
  perl,
  readline,
  versionCheckHook,
  xxhash,
  zlib,
  zydis,
}:

let
  # Both fields are managed automatically by ./update.sh.
  version = "6.2.4";
  srcHash = "sha256-AI2kA1ebEIb5pepvqeUycirk0MlOzF8fUCuo0qVFr8k=";

  # radare2 downloads these meson subprojects at configure time; Nix builds
  # are offline, so subprojects/ is prepopulated instead (same trick as the
  # nixpkgs package). The revisions must match this release's
  # subprojects/*.wrap files - if upstream bumps them in a release, the
  # update.sh build check will fail and roll back, and these pins need a
  # manual bump too. The binaryninja wrap is skipped: it's only used with
  # -Duse_v35=true (Binary Ninja plugin, needs proprietary libraries).
  sdbSrc = fetchFromGitHub {
    owner = "radareorg";
    repo = "sdb";
    rev = "2.5.8"; # https://github.com/radareorg/radare2/blob/6.2.4/subprojects/sdb.wrap
    hash = "sha256-WjjFDPKtv29wk1/Ktiv3XWACh4x5BmLOAyiI7t08qs0=";
  };

  qjsSrc = fetchFromGitHub {
    owner = "quickjs-ng";
    repo = "quickjs";
    rev = "b51e5278439b2fa0510fbb1f3b3dff9f0064d5d7"; # https://github.com/radareorg/radare2/blob/6.2.4/subprojects/qjs.wrap
    hash = "sha256-xKEtry1YJ6kQzGJRCpR9drSNlOYdMzLheVhdcxN0R8s=";
  };
in
stdenv.mkDerivation (finalAttrs: {
  pname = "radare2";
  inherit version;

  src = fetchFromGitHub {
    owner = "radareorg";
    repo = "radare2";
    rev = finalAttrs.version;
    hash = srcHash;
  };

  # Full plugin/feature set: radare2's meson feature options (threads, qjs,
  # panels, mmc, dylink, debugger, network, ...) default to on, and like
  # nixpkgs we use system libraries instead of bundled ones.
  mesonFlags = [
    (lib.mesonBool "use_sys_capstone" true)
    (lib.mesonBool "use_sys_lz4" true)
    (lib.mesonBool "use_sys_magic" true)
    (lib.mesonBool "use_sys_openssl" true)
    (lib.mesonBool "use_sys_xxhash" true)
    (lib.mesonBool "use_sys_zip" true)
    (lib.mesonBool "use_sys_zydis" true)
    (lib.mesonBool "use_sys_zlib" true)
    (lib.mesonOption "r2_gittap" finalAttrs.version)
  ];

  # Upstream's meson build forgets about the uxn arch plugin: it exists in
  # libr/arch/p/uxn and in the classic make build's plugin list
  # (dist/plugins-cfg/plugins.def.cfg), but not in the meson plugin/source
  # lists, so meson-built radare2 (e.g. nixpkgs') has no `asm.arch=uxn`.
  # The patch registers it in both places so it gets compiled and listed in
  # R_ARCH_STATIC_PLUGINS. If the hunk context breaks on a future release,
  # update.sh's build check will fail loudly and roll back.
  #
  # The second patch fixes the uxn decoder, which upstream got badly wrong:
  # it treated ops >= 0x80 and 0x2c-0x2e as immediate-carrying 2/3-byte
  # instructions, so any LIT desynced the whole stream (e.g. `80 68` showed
  # as `lit 0x6880`). Per the spec only jci/jmi/jsi (8-bit relative, resolved
  # to absolute addresses here) and lit/litr/lit2/lit2r have operands. Also
  # reports proper CJMP/JMP/CALL op types with jump targets, so `af` can
  # follow control flow.
  #
  # The third patch demotes the "Cannot find asm.parser for <arch>" warning
  # (and its "Fallback to null" INFO) to DEBUG: most arch plugins have no
  # parse plugin (which only does mnemonic text munging), so the warning on
  # every arch switch is pure noise.
  #
  # And an sdb opcode-description database for uxn (aod/aoda), shipped as
  # ./uxn.sdb.txt (all 256 mnemonics incl. mode-suffix variants) plus a list
  # entry to get it compiled and installed.
  patches = [
    ./uxn-arch-plugin.patch
    ./uxn-fix-disasm.patch
    ./uxn-quiet-parser-warning.patch
    ./uxn-opdesc.patch
  ];

  postPatch = ''
    cp ${./uxn.sdb.txt} libr/asm/d/uxn.sdb.txt
  '';

  enableParallelBuilding = true;

  strictDeps = true;

  nativeBuildInputs = [
    pkg-config
    meson
    ninja
    python3
  ];

  buildInputs = [
    capstone
    file
    libewf
    libusb-compat-0_1
    libuv
    lz4
    openssl
    perl
    readline
    zlib
    zydis
  ];

  propagatedBuildInputs = [
    # radare2 exposes r_lib which depends on these libraries
    file # for its list of magic numbers (`libmagic`)
    libzip
    xxhash
  ];

  postUnpack = ''
    pushd $sourceRoot/subprojects

    cp -r ${sdbSrc} sdb
    chmod -R +w sdb

    cp -r ${qjsSrc} qjs
    chmod -R +w qjs
    cp packagefiles/qjs/meson.build qjs

    popd
  '';

  postFixup = lib.optionalString stdenv.hostPlatform.isDarwin ''
    install_name_tool -add_rpath $out/lib $out/lib/libr_io.${finalAttrs.version}.dylib
  '';

  nativeInstallCheckInputs = [ versionCheckHook ];
  versionCheckProgramArg = "-v";
  doInstallCheck = true;

  meta = {
    description = "UNIX-like reverse engineering framework and command-line toolset";
    homepage = "https://radare.org";
    changelog = "https://github.com/radareorg/radare2/releases/tag/${finalAttrs.version}";
    license = with lib.licenses; [
      gpl3Only
      lgpl3Only
    ];
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "r2";
  };
})
