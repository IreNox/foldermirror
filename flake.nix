{
  description = "Selective hardlink folder mirroring with a local web UI";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f (import nixpkgs { inherit system; }));
    in {
      packages = forAllSystems (pkgs: {
        default = pkgs.buildGoModule {
          pname = "foldermirror";
          version = "0.1.0";
          src = self;
          vendorHash = null;
        };
      });
      apps = forAllSystems (pkgs: {
        default = { type = "app"; program = "${self.packages.${pkgs.system}.default}/bin/foldermirror"; };
      });
      devShells = forAllSystems (pkgs: { default = pkgs.mkShell { packages = [ pkgs.go pkgs.gopls ]; }; });
    };
}
