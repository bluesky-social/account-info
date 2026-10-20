{
  description = "account.info development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    nixpkgs-go.url = "github:NixOS/nixpkgs/nixos-26.05";
    systems.url = "github:nix-systems/default";
  };

  outputs = { nixpkgs, nixpkgs-go, systems, ... }:
    let
      eachSystem = nixpkgs.lib.genAttrs (import systems);
    in
    {
      devShells = eachSystem (system:
        let
          pkgs = import nixpkgs { inherit system; };
          goPkgs = import nixpkgs-go { inherit system; };
          goVersion = "1.26.10";
          # The pinned Nix channel can lag Go security patch releases.
          go = let
            patchedGo = goPkgs.go_1_26.overrideAttrs {
              version = goVersion;
              src = goPkgs.fetchurl {
                url = "https://dl.google.com/go/go${goVersion}.src.tar.gz";
                hash = "sha256-/WPog7ovK/ypPBZGsx6+9j4rHk0rMXyXidl2po2B828=";
              };
            };
          in
          assert pkgs.lib.assertMsg (patchedGo.version == goVersion)
            "nixpkgs-go must provide Go ${goVersion}, got ${patchedGo.version}";
          patchedGo;
          golangci-lint = pkgs.buildGoModule {
            pname = "golangci-lint";
            version = "2.10.1";
            src = pkgs.fetchFromGitHub {
              owner = "golangci";
              repo = "golangci-lint";
              rev = "v2.10.1";
              hash = "sha256-rHttQ+QJ9JrFvgfoX68Y0lD6BUv/aoOpRRFvZ1BIGIs=";
            };
            vendorHash = "sha256-yREpROQJ300+mii7R2oiyDjOGcYXBpv3o/park0TJYE=";
            subPackages = [ "cmd/golangci-lint" ];
            ldflags = [ "-s" "-w" "-X main.version=2.10.1" ];
          };
        in
        {
          default = pkgs.mkShell {
            packages = [
              go
              pkgs.git
              pkgs.just
              golangci-lint
            ];

            GOTOOLCHAIN = "local";

            shellHook = ''
              export GOTOOLCHAIN=local
              echo "account.info dev shell: Go ${goVersion}, golangci-lint 2.10.1"
            '';
          };
        });
    };
}
