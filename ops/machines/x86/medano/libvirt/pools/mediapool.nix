{ nixvirt, ... }:
{
  virtualisation.libvirt.connections."qemu:///system".pools = [
    {
      active = true;
      restart = true; # null == only on change
      definition = nixvirt.lib.pool.writeXML {
        name = "mediapool";
        uuid = "91E408C0-AB6B-4C37-BD51-8B2259354903";
        type = "zfs";
        source.name = "grave/media";
      };
    }
  ];
}
