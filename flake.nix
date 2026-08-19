{
  description = "sitka development shell";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.05";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forEachSystem = f:
        builtins.listToAttrs (map (system: {
          name = system;
          value = f system;
        }) systems);
    in {
      packages = forEachSystem (system:
        let pkgs = import nixpkgs { inherit system; };
        in {
          default = pkgs.buildGo125Module {
            pname = "sitka";
            version = "0.1.0";
            src = self;
            vendorHash = "sha256-feVc17HT/eYnDhn2KnbgOfCw9NH4D/wl8oTvVEhVWF0=";
            subPackages = [ "cmd/sitka" ];
            meta.mainProgram = "sitka";
          };
        });

      devShells = forEachSystem (system:
        let
          pkgs = import nixpkgs { inherit system; };
          # golangci-lint refuses to lint a module whose Go version is newer
          # than the one it was built with, so rebuild it against go_1_25.
          golangciLint = pkgs.golangci-lint.override {
            buildGo124Module = pkgs.buildGo125Module;
          };
        in {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go_1_25
              gopls
              gotools
              golangciLint
              git
            ];

            shellHook = ''
              export PATH="$PWD/bin:$PATH"
              echo "sitka devShell: $(go version | awk '{ print $3 }')"
            '';
          };
        });
    };
}
