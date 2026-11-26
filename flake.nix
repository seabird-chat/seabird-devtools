{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
  };

  outputs =
    inputs@{
      nixpkgs,
      flake-parts,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = nixpkgs.lib.systems.flakeExposed;
      perSystem =
        { pkgs, self', ... }:
        let
          formatters = [
            pkgs.gotools
            pkgs.nixfmt
          ];
        in
        {
          formatter = pkgs.treefmt.withConfig {
            runtimeInputs = formatters;
            configFile = ./treefmt.toml;
          };

          packages = {
            default = self'.packages.seabird-devtools;

            seabird-devtools = pkgs.buildGoModule rec {
              pname = "seabird-devtools";
              version = "0.1.0-dev";
              src = ./.;
              vendorHash = "sha256-baVa5B/ym9jAnc5C3DywwjnHwqPfFQXKQ3CNtJ7C7OU=";
              subPackages = [ "cmd/seabird-dev" ];
              checkFlags = [ "-timeout=60s" ];

              ldflags = [
                "-s"
                "-w"
                "-X main.Version=v${version}"
              ];

              env.CGO_ENABLED = 0;
            };
          };

          devShells.default = pkgs.mkShell {
            packages = [
              pkgs.go
              pkgs.gopls
              pkgs.golangci-lint

              pkgs.just
              pkgs.treefmt
            ]
            ++ formatters;
          };
        };
    };
}
