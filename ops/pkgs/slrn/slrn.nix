{ lib
, stdenv
, fetchgit
, slang
}:

stdenv.mkDerivation {
  pname = "slrn";
  version = "git";

  src = fetchgit {
    url = "git://git.jedsoft.org/git/slrn.git";
    rev = "HEAD";
    sha256 = "sha256-VxjH6hgpmdQJIKZrojHZuLRznnh7SVUif8IyXlla0OE=";
  };

  buildInputs = [ slang ];

  NIX_CFLAGS_COMPILE = "-Wno-implicit-function-declaration -Wno-implicit-int";

  configureFlags = [
    "--with-slanginc=${slang.dev or slang}/include"
    "--with-slanglib=${slang.out or slang}/lib"
  ];

  enableParallelBuilding = true;
  enableParallelInstalling = false;

  meta = with lib; {
    description = "Threaded, S-Lang based Usenet/NNTP newsreader";
    homepage = "https://www.jedsoft.org/slrn/";
    license = licenses.gpl2Plus;
    platforms = platforms.unix;
    mainProgram = "slrn";
  };
}
