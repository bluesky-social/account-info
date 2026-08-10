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
          goVersion = "1.26.5";
          go = assert pkgs.lib.assertMsg (goPkgs.go_1_26.version == goVersion)
            "nixpkgs-go must provide Go ${goVersion}, got ${goPkgs.go_1_26.version}";
            goPkgs.go_1_26;
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
