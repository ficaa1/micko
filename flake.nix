{
  description = "Keyboard-first terminal UI for Argo Workflows";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          micko = pkgs.callPackage ./nix/package.nix {
            src = self;
            revision = self.shortRev or "dirty";
          };
          default = self.packages.${system}.micko;
        }
      );

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.micko}/bin/micko";
        };
      });

      checks = forAllSystems (system: {
        micko = self.packages.${system}.micko;
      });
    };
}
