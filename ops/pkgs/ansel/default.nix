# Ansel — a darktable fork (https://ansel.photos), "minus the bloat".
#
# Modeled on nixpkgs' darktable package (pkgs/by-name/da/darktable), adapted
# to Ansel's actual CMake probe surface: no lua, SDL2, portmidi, gmic,
# GraphicsMagick, libsecret, libgphoto2 or jasper any more; Exiv2, LibRaw,
# RawSpeed and the OpenCL headers are always the vendored git submodules;
# the lens database and the neural-denoise models are normally downloaded at
# configure time and are prefetched here instead (nix builds have no network).
#
# Pinned to a nightly commit from the "fairly stable" channel; version string
# follows upstream's own git-describe naming so the app reports the same
# version as the official nightlies. Which commit was picked, and why, is
# recorded next to `version` below.
{ ... }:
{
  lib,
  stdenv,
  fetchFromGitHub,
  fetchurl,
  linkFarm,

  # nativeBuildInputs
  cmake,
  desktop-file-utils,
  gettext,
  intltool,
  libxslt, # xsltproc (ansel's primary XSLT path; Saxon is only the fallback)
  llvmPackages,
  ninja,
  perl, # pod2man for the man pages, plus build-time scripts
  pkg-config,
  python3, # tools/generate_iop_static.py runs at configure time
  wrapGAppsHook3,

  # buildInputs
  adwaita-icon-theme,
  cairo,
  cmark,
  colord,
  colord-gtk,
  cups,
  curl,
  expat,
  glib,
  glib-networking,
  gtk3,
  gtk-mac-integration,
  icu,
  isocodes,
  json-glib,
  lcms2,
  lensfun,
  libavif,
  libheif,
  libiconv,
  libjpeg,
  libpng,
  libwebp,
  librsvg,
  libsysprof-capture,
  libtiff,
  libxml2,
  ocl-icd,
  openexr,
  openjpeg,
  osm-gps-map,
  pango,
  pugixml,
  sqlite,
  zlib,
}:

let
  pugixml-shared = pugixml.override { shared = true; };

  # Assets the build otherwise downloads at configure time (FETCH_LENS_DB /
  # FETCH_NN_MODELS). Pinned by content; URLs pinned by commit so the files
  # stay fetchable even when upstream's moving `main` ships newer ones.

  # Lens calibration DB ("LensSerious"), schema v4 matches the submodule pin.
  lensDbCommit = "80a79c06a1eb159aebbd50ad958e9923c2bfeb3a"; # LensSerious main, 2026-10-09
  lensDbUrl = file: sha256:
    fetchurl {
      url = "https://raw.githubusercontent.com/aurelienpierreeng/LensSerious/${lensDbCommit}/db/v4/${file}";
      inherit sha256;
    };
  lensesDb = lensDbUrl "lenses.db" "sha256-uG/1Dq4bE5GdskEDQ9NWgtQrb5ygmn4t1HGvwulcUv4=";
  lensfunXml = lensDbUrl "lensfun-xml.tar.xz" "sha256-h6803w+acb5VEuSJpizwfRLPZUaR4LwLmM8lO2aIPMM=";

  # Neural raw-denoise models (module `rawdenoiseai` hides itself without them).
  nnModelsCommit = "c2b4034f8df17a1af171fa5dee5ef9d17a3afccf"; # ansel-denoise master, 2026-08-02
  nnModel = file: sha256:
    fetchurl {
      name = file; # keep the store path name = the upstream file name
      url = "https://raw.githubusercontent.com/aurelienpierreeng/ansel-denoise/${nnModelsCommit}/models/${file}";
      inherit sha256;
    };
  nnModelFiles = [
    (nnModel "denoise-half-multi-v1.anselnn" "sha256-DXuxiRAdHC8NwPGxSl+K+RiPHPB5+kMI9TYwEzLdRc4=")
    (nnModel "denoise-half-single-v1.anselnn" "sha256-FlDYJ9cq0FmQ376ZTCMqCLWEdwpSzTKdWMaZPfA7qm0=")
    (nnModel "denoise-large-multi-v1.anselnn" "sha256-oSklts89fzH6/L0F0T/ZmhPmTOi9Cc8Y16vH4+2gOtE=")
    (nnModel "denoise-large-single-v1.anselnn" "sha256-vF72Fjej1jkCvuLTZ5leix0SGdEGImhFjqcdVQpDrjU=")
    (nnModel "denoise-quarter-multi-v1.anselnn" "sha256-40muuLNaf7JIMupluxfbkV2dMPcKL1l1rbV5Tjez0X8=")
    (nnModel "denoise-quarter-single-v1.anselnn" "sha256-dc8XonFl7itGncztW6FCYE8DDn6NDLiJqmehKn85y0k=")
  ];
  # rawdenoiseai composes the file names ("denoise-<size>-<scale>-v1.anselnn")
  # and opens them from $<datadir>/ansel, so they must be installed under
  # exactly those names; a plain `cp $storePath` would keep the nix store's
  # hash prefix. A linkFarm re-exposes them under just their upstream names.
  nnModels = linkFarm "ansel-nn-models" (
    map (m: {
      inherit (m) name;
      path = m;
    }) nnModelFiles
  );
in
stdenv.mkDerivation (finalAttrs: {
  pname = "ansel";
  # Nightly "0.0.0+5241.g7230d1b355" (2026-10-08): the most broadly tested
  # recent build on the download page at packaging time (100% crash-free on
  # Linux/macOS), rather than the newest one-day-old build.
  version = "0.0.0+5241.g7230d1b355";

  src = fetchFromGitHub {
    owner = "aurelienpierreeng";
    repo = "ansel";
    rev = "7230d1b35573214c4832cd3006d4ea6a44855283";
    # Submodules are required: rawspeed, LibRaw(-cmake), exiv2, LensSerious,
    # whereami and the OpenCL headers all live in them; the cmake configure
    # hard-fails without rawspeed/whereami/LensSerious.
    fetchSubmodules = true;
    hash = "sha256-YNcS/bhVcO1WmOqgXWKlwB1L8571bvmKuzAJJh694lk=";
  };

  # The checkout has no .git (fetchFromGitHub deletes it), so CMake treats it
  # as a "source package" and tools/create_version_c.sh would otherwise write
  # "unknown-version"; these env vars are its documented override.
  ANSEL_VERSION = finalAttrs.version;
  ANSEL_COMMIT_HASH = finalAttrs.src.rev;

  # src/libs/collect.c includes <gio/gunixmounts.h> on every non-Windows
  # platform, but nothing in the build ever adds glib's gio-unix-2.0 include
  # dir (upstream's CI gets away with it; sealed build environments don't).
  env.NIX_CFLAGS_COMPILE = toString [
    "-I${glib.dev}/include/gio-unix-2.0"
  ];

  nativeBuildInputs = [
    cmake
    desktop-file-utils
    gettext
    intltool
    libxslt
    ninja
    perl
    pkg-config
    python3
    wrapGAppsHook3
  ];

  buildInputs = [
    adwaita-icon-theme
    cairo
    cmark
    cups
    curl
    expat
    glib
    glib-networking
    gtk3
    icu
    isocodes
    json-glib
    lcms2
    lensfun
    libavif
    libheif
    libjpeg
    libpng
    libwebp
    librsvg
    libsysprof-capture
    libtiff
    libxml2
    openexr
    openjpeg
    osm-gps-map
    pango
    pugixml-shared
    sqlite
    zlib
  ]
  ++ lib.optionals stdenv.hostPlatform.isLinux [
    colord
    colord-gtk
    ocl-icd
  ]
  ++ lib.optional stdenv.hostPlatform.isDarwin gtk-mac-integration
  # Bundled Exiv2's iconv probe needs a real libiconv on Darwin (glibc covers
  # it on Linux). libomp for OpenMP under nixpkgs' clang.
  ++ lib.optionals stdenv.hostPlatform.isDarwin [ libiconv ]
  ++ lib.optional stdenv.cc.isClang llvmPackages.openmp;

  # Stage the lens DB OUTSIDE the source tree at configure time: CMake's
  # in-source-build guard rejects the lensfun-xml ARCHIVE_EXTRACT destination
  # if LENS_DB_DIR lives under CMAKE_SOURCE_DIR, and the extract needs a
  # writable dir anyway (the store is read-only).
  preConfigure = ''
    lensDbStage="$TMPDIR/ansel-lens-db"
    mkdir -p "$lensDbStage"
    cp ${lensesDb} "$lensDbStage/lenses.db"
    cp ${lensfunXml} "$lensDbStage/lensfun-xml.tar.xz"

    cmakeFlagsArray+=(
      "-DFETCH_LENS_DB=OFF" "-DLENS_DB_DIR=$lensDbStage"
    )
  '';

  cmakeFlags = [
    # generic `-march` instead of `-march=native`
    "-DBINARY_PACKAGE_BUILD=ON"
    # no opt-in crash/usage reporting in a packaged build
    "-DUSE_SENTRY=OFF"
    "-DUSE_TELEMETRY=OFF"
    "-DBUILD_CHANNEL=package-nixos"
    # NN models: fetched + name-normalized by nix, not downloaded by cmake
    "-DFETCH_NN_MODELS=OFF"
    "-DNN_MODELS_DIR=${nnModels}"
    # don't test-compile the OpenCL kernels at build time (skips the LLVM
    # build dependency); they are JIT-compiled on first use at runtime
    "-DTESTBUILD_OPENCL_PROGRAMS=OFF"
  ]
  ++ lib.optionals stdenv.hostPlatform.isDarwin [
    "-DUSE_COLORD=OFF"
  ];

  # ansel, like darktable (>= its rpath rework), loads lib_ansel.so and its
  # plugins at runtime and JIT-compiles the .cl kernels in place; give the
  # wrapped binaries $out/lib{,/ansel} and absolutize the kernel includes.
  preFixup =
    let
      libPathEnvVar = if stdenv.hostPlatform.isDarwin then "DYLD_LIBRARY_PATH" else "LD_LIBRARY_PATH";
      libPathPrefix =
        "$out/lib:$out/lib/ansel"
        + lib.optionalString stdenv.hostPlatform.isLinux ":${ocl-icd}/lib";
    in
    ''
      for f in $out/share/ansel/kernels/*.cl; do
        sed -r "s|#include \"(.*)\"|#include \"$out/share/ansel/kernels/\1\"|g" -i "$f"
      done

      gappsWrapperArgs+=(
        --prefix ${libPathEnvVar} ":" "${libPathPrefix}"
      )
    '';

  meta = {
    description = "Virtual lighttable and darkroom for photographers (darktable fork)";
    homepage = "https://ansel.photos";
    license = lib.licenses.gpl3Plus;
    platforms = with lib.platforms; linux ++ darwin;
    mainProgram = "ansel";
  };
})
