{ nixvirt, ... }:
{
  virtualisation.libvirt.connections."qemu:///system".pools = [
    {
      active = true;
      restart = true; # null == only on change
      definition = (
        nixvirt.lib.pool.writeXML {
          name = "vmpool";
          uuid = "90E33DA4-2456-4452-91CA-382CB5043805";
          type = "dir";
          target.path = "/keep/pools/vmpool";
        }
      );
    }
  ];
}
