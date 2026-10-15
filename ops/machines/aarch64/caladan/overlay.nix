{ ... }:

{
  nixpkgs = {
    overlays = [
      (self: super: {
        claude-code = super.claude-code.override {
          manifest = builtins.fromJSON (builtins.readFile ./claude-code-manifest.json);
        };
      })
    ];
    config = {
      allowUnfree = true;
      allowBroken = true;
    };
  };
}
