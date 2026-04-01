{ nixvirt, ... }:
{
  virtualisation.libvirt.connections."qemu:///system".pools = [
    {
      active = true;
      restart = true; # null == only on change
      definition = (
        nixvirt.lib.pool.writeXML {
          name = "isopool";
          uuid = "344BB9E3-978B-4E59-9329-7B4F8747127A";
          type = "dir";
          target.path = "/keep/pools/isopool";
        }
      );
    }
  ];
}
