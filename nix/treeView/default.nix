let
  # 1. Import the root layout
  hefe = import ../../default.nix { };

  # Check for buildable targets
  isDerivation = x: builtins.isAttrs x && x ? type && x.type == "derivation";

  # 2. Traverse with an explicit cycle-tracker ('seen') and a 'depth' counter
  gatherTargets = prefix: node: seen: depth:
    # Safe escape hatch if an un-marked attribute set loops infinitely or goes too deep
    if !builtins.isAttrs node || depth > 100 then [ ]
    else
      let
        # Normalize the actual readTree path tracking attribute if present
        nodePath = if node ? __readTree 
                   then "//" + (builtins.concatStringsSep "/" node.__readTree) 
                   else prefix;
      in
      # If we have already traversed this exact structural path in this branch, break the cycle
      if builtins.elem nodePath seen then [ ]
      else
        let
          # Check if the current node is a direct derivation
          thisTarget = if isDerivation node then [ prefix ] else [ ];

          # Extract subtargets (like meta.ci.targets)
          subTargets =
            let targets = (node.meta.targets or [ ]) ++ (node.meta.ci.targets or [ ]);
            in builtins.map (sub: "${prefix}:${sub}") targets;

          # Get children explicitly tracked by readTree
          childrenNames = node.__readTreeChildren or [ ];
          
          childTargets = builtins.concatMap (name:
            let 
              nextPrefix = if prefix == "//" then "//${name}" else "${prefix}/${name}";
            in
            # Pass the updated seen list and increment depth
            gatherTargets nextPrefix node.${name} (seen ++ [ nodePath ]) (depth + 1)
          ) childrenNames;
        in
        thisTarget ++ subTargets ++ childTargets;
in
# 3. Initialize evaluation from the root with empty tracking
gatherTargets "//" hefe [ ] 0
