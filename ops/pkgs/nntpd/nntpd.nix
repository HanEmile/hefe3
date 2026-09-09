{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "nntpd";
  version = "0.0.1";

  src = ./.;

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  doCheck = false;

  meta = {
    description = "A minimal nntpd server";
    longDescription = ''
    	Fiddled around with some existing ones, decided to write my own
    '';
    homepage = "https://github.com/HanEmile/hefe3/tree/main/ops/pkgs/nntpd";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "nntpd";
  };
})
