{ nixvirt, ... }:

# logs in
# /var/log/libvirt/qemu/win1.log

{
  virtualisation.libvirt.connections."qemu:///system" = {
    domains = [
      {
        active = true;
        definition = nixvirt.lib.domain.writeXML (
          nixvirt.lib.domain.templates.windows {
            name = "win1";
            uuid = "7D824CBA-858E-4F75-BBF5-9F4054B7CD01";
            vcpu = {
              placement = "static";
              count = 4;
            };
            memory = {
              count = 8;
              unit = "GiB";
            };
            storage_vol = "/keep/pools/vmpool/win1.qcow2";
            install_vol = "/keep/pools/isopool/Win11_25H2_EnglishInternational_x64.iso";
            nvram_path = "/var/lib/libvirt/qemu/nvram/win11_VARS.fd";
            virtio_net = true;
            virtio_drive = true;
            install_virtio = true;
          }
        );
      }
    ];
  };
}
