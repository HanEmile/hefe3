{ hefe, ... }:

let
  sources = hefe.third_party;

  nixos = sources."nixos-26.05";

  pkgs = import nixos {
    system = "aarch64-darwin";
    config.allowUnfree = true;
  };
  lib = import (nixos + "/lib");

  imp = name: deps: import (sources."${name}".outPath + "/default.nix") deps;

  pyproject-nix = imp "pyproject.nix" { inherit lib; };
  pyproject-uv2nix = imp "uv2nix" { inherit pyproject-nix lib; };
  uv2nix = pyproject-uv2nix;
  pyproject-build-systems-pkgs = imp "build-system-pkgs" { inherit uv2nix pyproject-nix lib; };

  ida-pro-mcp-path = sources."ida-pro-mcp".outPath;

  python = pkgs.python313;

  # 1. Load Project Workspace (parses pyproject.toml, uv.lock)
  workspace = pyproject-uv2nix.lib.workspace.loadWorkspace {
    workspaceRoot = ida-pro-mcp-path;
  };

  # 2. Generate Nix Overlay from uv.lock (via workspace)
  uvLockedOverlay = workspace.mkPyprojectOverlay {
    sourcePreference = "wheel"; # Or "sdist"
  };

  # 3. Placeholder for Your Custom Package Overrides
  myCustomOverrides = final: prev: {
    # e.g., some-package = prev.some-package.overridePythonAttrs (...);
  };

  # 4. Construct the Final Python Package Set
  pythonSet =
    (pkgs.callPackage pyproject-nix.build.packages {
      inherit python;
    }).overrideScope
      (
        pkgs.lib.composeManyExtensions [
          pyproject-build-systems-pkgs.default # For build tools
          uvLockedOverlay # Your locked dependencies
          myCustomOverrides # Your fixes
        ]
      );

  # --- This is where your project's metadata is accessed ---
  projectNameInToml = "ida-pro-mcp"; # MUST match [project.name] in pyproject.toml!
  thisProjectAsNixPkg = pythonSet.${projectNameInToml};
  # ---

  # 5. Create the Python Runtime Environment
  appPythonEnv = pythonSet.mkVirtualEnv (thisProjectAsNixPkg.pname + "-env") workspace.deps.default; # Uses deps from pyproject.toml [project.dependencies]
in
pkgs.mkShell {
  shellHook = ''
    echo "Consider executing `ida-pro-mcp --install`"
  '';

  packages = [
    appPythonEnv
    pkgs.uv
  ];
}
