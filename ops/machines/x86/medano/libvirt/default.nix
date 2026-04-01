{ pkgs, ... }:

{

  imports = [
    ./networking
    ./pools
  ];

  virtualisation = {
    libvirtd = {
      enable = true;
      package = pkgs.libvirt;

      # List of bridge devices that can be used by qemu:///session
      allowedBridges = [
        "virbr0"
      ];

      qemu.swtpm.enable = true;
    };
    libvirt = {
      enable = true;
      verbose = true;
      swtpm.enable = true;
    };

    # TODO(emile): remove this, doesn't seem to work, idk why
    # forwardPorts =
    #   let
    #     # TODO(emile): define some centralized IPAM somewhere
    #     naraj = "192.168.75.2";
    #   in
    #   [
    #     {
    #       host.address = "127.0.0.1";
    #       host.port = 80;
    #       guest.address = naraj;
    #       guest.port = 80;
    #     }
    #     {
    #       host.address = "127.0.0.1";
    #       host.port = 443;
    #       guest.address = naraj;
    #       guest.port = 443;
    #     }
    #   ];
  };

  # Oct 19 03:46:29 medano nixvirt-start[714787]: NixVirt: libvirt error: internal error: process exited while connecting to monitor: MESA-LOADER: failed to open dri: /run/opengl-driver/lib/gbm/dri_gbm.so: cannot open shared object file: No such file or directory (search paths /run/opengl-driver/lib/gbm, suffix _gbm)
  hardware.graphics.enable = true;
}
