{ pkgs, ... }:
{ ... }:

pkgs.stdenv.mkDerivation rec {
  pname = "cataclysm-dda";
  version = "0.I";

  src = pkgs.fetchFromGitHub {
    owner = "CleverRaven";
    repo = "Cataclysm-DDA";
    rev = version;
    hash = "sha256-nzHqN6WjhuR1IoJ50XryI3B1fUQPepzGMaDJzudUaVI=";
  };

  nativeBuildInputs = with pkgs; [
    pkg-config
    git
    gettext
  ];

  buildInputs = with pkgs; [
    freetype
    libogg
    libvorbis
    SDL2
    SDL2_image
    SDL2_mixer
    SDL2_ttf
    zlib
  ];

  makeFlags = [
    "PREFIX=$(out)"
    "TILES=1"
    "SOUND=1"
    "LOCALIZE=1"
    "WERROR=0"  # <--- Suppresses treating warnings as errors
  ];

  NIX_CFLAGS_COMPILE = [
    "-Wno-error"
    "-Wno-missing-noreturn"
  ];

  postPatch = ''
    substituteInPlace src/item_search.cpp \
      --replace-fail "auto const error = [hint, filter]" "auto const error = [hint, filter] [[noreturn]]"
  '';

  meta = with pkgs.lib; {
    description = "Turn-based survival game set in a post-apocalyptic world";
    homepage = "https://github.com/CleverRaven/Cataclysm-DDA";
    license = licenses.cc-by-sa-30;
    mainProgram = "cataclysm-tiles";
    platforms = platforms.unix;
  };
}
