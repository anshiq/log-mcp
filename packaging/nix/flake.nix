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
      let pkgs = nixpkgs.legacyPackages.${system}; in {
        packages = {
          agent-runtime = pkgs.buildGoModule {
            pname = "agent-runtime";
            version = "3.0.0";
            src = ./.;
            vendorHash = null;
            subPackages = [
              "cmd/agent-runtime"
              "cmd/agentd"
              "cmd/agent-runtime-shim"
            ];
            ldflags = [ "-s" "-w" ];
          };
          default = self.packages.${system}.agent-runtime;
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
