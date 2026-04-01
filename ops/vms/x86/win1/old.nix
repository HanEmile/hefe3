{ nixvirt, lib, ... }:

# logs in
# /var/log/libvirt/qemu/win1.log

let
  drive_address = unit: {
    type = "drive";
    controller = 0;
    bus = 0;
    target = 0;
    inherit unit;
  };
in
{
  virtualisation.libvirt.connections."qemu:///system" = {
    domains = [
      {
        active = true;
        definition =
          let
            win1 = nixvirt.lib.domain.templates.windows {
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
              install_vol = "/keep/pools/isopool/Win11_25H2_EnglishInternational_x64.iso";
              nvram_path = "/var/lib/libvirt/qemu/nvram/win11_VARS.fd";
              no_graphics = true;
              virtio_net = true;
            };
          in
          (nixvirt.lib.domain.writeXML (
            win1
            // {
              cpu = {
                mode = "host-passthrough";
                check = "none";
                migratable = false;
                topology = {
                  sockets = 1;
                  dies = 1;
                  cores = 2;
                  threads = 2;
                };
                # cache = {
                #   mode = "passthrough";
                # };
                feature = [
                  {
                    policy = "require";
                    name = "topoext";
                  }
                  {
                    policy = "disable";
                    name = "hypervisor";
                  }
                ];
              };
              iothreads = {
                count = 1;
              };
              clock = win1.clock // {
                timer =
                  lib.lists.remove {
                    name = "hpet";
                    present = false;
                  } win1.clock.timer
                  ++ [
                    {
                      name = "hpet";
                      present = true;
                    }
                  ];
              };
              os = win1.os // {
                boot = null;
                bootmenu = {
                  enable = false;
                };
                smbios = {
                  mode = "host";
                };
              };
              features = win1.features // {
                kvm = {
                  hidden.state = true;
                };
                hyperv = win1.features.hyperv // {
                  vendor_id = {
                    state = true;
                    value = "1234567890ab";
                  };
                };
              };

              devices = win1.devices // {
                inherit (import ../libvirt-base.nix)
                  serial
                  console
                  channel
                  # graphics
                  video
                  audio
                  redirdev
                  ;

                # tpm = {
                #   model = "tpm-tis";
                #   backend = {
                #     type = "passthrough";
                #     device = {
                #       path = "/dev/tpm0";
                #     };
                #   };
                # };

                graphics = {
                  type = "spice";
                  autoport = true;
                  listen = {
                    type = "address";
                  };
                  image = {
                    compression = false;
                  };
                  gl = {
                    enable = false;
                  };
                };

                interface = [
                  {
                    type = "bridge";
                    model = {
                      type = "virtio";
                    };
                    source = {
                      bridge = "virbr0"; # default
                    };
                  }
                ];

                disk = win1.devices.disk ++ [

                  # the "main" drive
                  {
                    type = "file";
                    device = "disk";
                    driver = {
                      name = "qemu";
                      type = "qcow2";
                      cache = "writeback";
                    };
                    source = {
                      # cd /keep/pools/vmpool && qemu-img create -f qcow2 win1.qcow2 150G
                      file = /keep/pools/vmpool/win1.qcow2;
                    };
                    target = {
                      bus = "sata";
                      dev = "sda";
                    };
                    address = drive_address 0;
                  }

                  # the unattended xml from https://schneegans.de/windows/unattend-generator
                  {
                    type = "file";
                    device = "cdrom";
                    driver = {
                      name = "qemu";
                      type = "raw";
                    };
                    source = {
                      file = /keep/pools/vmpool/unattended.iso;
                      startupPolicy = "mandatory";
                    };
                    target = {
                      bus = "sata";
                      dev = "sdb";
                    };
                    readonly = true;
                    address = drive_address 1;
                  }
                ];
              };
            }
          ));
      }
    ];
  };
}
