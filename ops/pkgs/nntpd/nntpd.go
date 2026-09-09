{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "nntpd";
  version = "0.0.1";

  src = ./.;

  # vendorHash = "sha256-jO9d+wJr03rqlPrQ3mmWOxOXw2kL+0x8YkkXu/Msm+Q=";
  vendorHash = null;

  modRoot = ".";
  subPackages = [
    "."
  ];

  # Test files are not part of the release tarball
  doCheck = false;

  meta = {
    description = "A minimal nntpd server";
    longDescription = ''
    	Fiddled around with some existing ones, decided to write my own
    '';
    homepage = "https://github.com/hanemile/hefe3";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
    mainProgram = "nntpd";
  };
})
