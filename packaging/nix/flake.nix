# agent-runtime Nix flake: packages + Home Manager module.
# The primary dev box is NixOS; `services.agent-runtime.enable` installs
# the user units, matching `agent-runtime daemon install`.
{
  description = "agent-runtime: local async process supervisor for AI coding agents";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        version = "3.0.0";
      in {
        packages = {
          agent-runtime = pkgs.buildGoModule {
            pname = "agent-runtime";
            inherit version;
            # This flake lives in packaging/nix; the Go module is the repo root.
            src = ../..;
            # Real hash (no vendor/ dir): refresh after go.mod/go.sum changes with
            #   nix build ./packaging/nix#agent-runtime  # read expected hash from error
            vendorHash = "sha256-4cNfrFOqa187gT3mR3j0REFRKUGB3zB6Owzb0pJguD8=";
            subPackages = [
              "cmd/agent-runtime"
              "cmd/agentd"
              "cmd/agent-runtime-shim"
            ];
            ldflags = [
              "-s"
              "-w"
              "-X agent-runtime/internal/mcp.Version=${version}"
              "-X main.version=${version}"
            ];
          };
          default = self.packages.${system}.agent-runtime;
        };
        devShells.default = pkgs.mkShell {
          # cmd/agent-runtime-gui (Wails v2 + systray) needs cgo and native
          # dev headers not present outside this shell. `make build` (no
          # gui) doesn't need any of this; only `make build-gui` does:
          #   nix develop ./packaging/nix -c make build-gui
          packages = with pkgs; [
            go
            nodejs
            pkg-config
            gtk3
            webkitgtk_4_1
            libayatana-appindicator
          ];
        };
      }) // {
        homeManagerModules.default = { config, lib, pkgs, ... }:
          let cfg = config.services.agent-runtime; in {
            options.services.agent-runtime = {
              enable = lib.mkEnableOption "agent-runtime per-user daemon";
              package = lib.mkPackageOption pkgs "agent-runtime" { };
            };
            config = lib.mkIf cfg.enable {
              home.packages = [ cfg.package ];
              systemd.user.sockets.agentd = {
                Unit.Description = "agent-runtime daemon socket";
                Socket = {
                  ListenStream = "%t/agent-runtime/agentd.sock";
                  SocketMode = "0600";
                  DirectoryMode = "0700";
                };
                Install.WantedBy = [ "sockets.target" ];
              };
              systemd.user.services.agentd = {
                Unit = {
                  Description = "agent-runtime daemon";
                  Requires = [ "agentd.socket" ];
                  After = [ "agentd.socket" ];
                };
                Service = {
                  Type = "notify";
                  KillMode = "process";
                  Delegate = true;
                  Restart = "on-failure";
                  ExecStart = "${cfg.package}/bin/agentd --foreground";
                };
                Install.WantedBy = [ "default.target" ];
              };
            };
          };
      };
}
