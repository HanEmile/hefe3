{ ... }:
{
  lib,
  buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "lora8";
  version = "0.1.0";

  src = lib.cleanSourceWith {
    src = ./.;
    # vendor/ is checked in and must survive
    filter = path: type: true;
  };

  vendorHash = null;

  modRoot = ".";
  subPackages = [ "." ];

  env.CGO_ENABLED = 0;

  meta = {
    description = "Tooling + web UI for the MikroTik R11e-LR8 (SX1301) LoRa concentrator";
    license = lib.licenses.bsd3;
    mainProgram = "lora8";
  };
})
