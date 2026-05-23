{ nixvirt }:
{
  name,
  uuid,
  memory,
  install_vol ? null,
  vmdisk,
  interfaces ? [ "virbr0" ],
  vcpu_count ? 4,
  graphics ? null,
}:
{
  virtualisation.libvirt.connections."qemu:///system" = {
    domains = [
      {
        active = true;
        definition =
          let
            vmdef = nixvirt.lib.domain.templates.linux {
              inherit name uuid install_vol;
              vcpu.count = vcpu_count;
              memory.unit = "GiB";
              memory.count = memory;
            };
          in
          (nixvirt.lib.domain.writeXML (
            vmdef
            // {
              devices = {
                serial.type = "pty";
                serial.target.port = 0;

                console.type = "pty";
                console.target.type = "virtio";
                console.target.port = 0;

                channel = null;
                inherit graphics;

                video.model.type = "virtio";
                video.model.vram = 32768;
                video.model.heads = 1;
                video.model.primary = true;

                audio = null;
                redirdev = null;

                interface = builtins.map (br: {
                  type = "bridge";
                  model.type = "virtio";
                  source.bridge = br;
                }) interfaces;

                disk = vmdef.devices.disk ++ [
                  {
                    type = "file";
                    device = "disk";
                    driver.name = "qemu";
                    driver.type = "qcow2";
                    driver.cache = "writeback";
                    source.file = vmdisk;
                    target.bus = "sata";
                    target.dev = "sda";
                    address.type = "drive";
                    address.controller = 0;
                    address.bus = 0;
                    address.target = 0;
                    address.unit = 0;
                  }
                ];
              };
            }
          ));
      }
    ];
  };
}
