{ self }:
{ config, lib, pkgs, utils, ... }:

let
  cfg = config.services.foldermirror;
  command = [
    (lib.getExe cfg.package)
    "-storage"
    cfg.storage
    "-mirror"
    cfg.mirror
    "-listen"
    cfg.listen
  ]
  ++ lib.optionals (cfg.imports != null) [ "-imports" cfg.imports ]
  ++ lib.optionals (cfg.stateFile != null) [ "-state" cfg.stateFile ];
in
{
  options.services.foldermirror = {
    enable = lib.mkEnableOption "FolderMirror hardlink organizer";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      defaultText = lib.literalExpression "inputs.foldermirror.packages.\${pkgs.stdenv.hostPlatform.system}.default";
      description = "The FolderMirror package to run.";
    };

    storage = lib.mkOption {
      type = lib.types.str;
      example = "/srv/media/storage";
      description = "Existing storage directory containing the files to organize.";
    };

    mirror = lib.mkOption {
      type = lib.types.str;
      example = "/srv/media/mirror";
      description = "Mirror directory. It must be on the same filesystem as storage.";
    };

    imports = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/srv/media/imports";
      description = "Optional directory scanned by Collect files. It must be on the same filesystem as storage.";
    };

    listen = lib.mkOption {
      type = lib.types.str;
      default = "127.0.0.1:8787";
      description = "Address and port on which the web interface listens.";
    };

    stateFile = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "/var/lib/foldermirror/state.json";
      description = "Optional state file path. By default FolderMirror stores .foldermirror.json in the mirror directory.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "foldermirror";
      description = "User account under which the service runs.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "foldermirror";
      description = "Primary group under which the service runs.";
    };

    extraGroups = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [ ];
      example = [ "media" ];
      description = "Supplementary groups granted to the automatically created service user.";
    };

    createUser = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Whether to create the configured system user and group.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = cfg.storage != cfg.mirror;
        message = "services.foldermirror.storage and services.foldermirror.mirror must be different directories";
      }
    ];

    users = lib.mkIf cfg.createUser {
      groups.${cfg.group} = { };
      users.${cfg.user} = {
        isSystemUser = true;
        group = cfg.group;
        extraGroups = cfg.extraGroups;
        description = "FolderMirror service user";
      };
    };

    systemd.services.foldermirror = {
      description = "FolderMirror hardlink organizer";
      documentation = [ "https://github.com/IreNox/foldermirror" ];
      wantedBy = [ "multi-user.target" ];
      after = [ "local-fs.target" ];
      requiresMountsFor = [ cfg.storage cfg.mirror ]
        ++ lib.optional (cfg.imports != null) cfg.imports
        ++ lib.optional (cfg.stateFile != null) (builtins.dirOf cfg.stateFile);

      serviceConfig = {
        ExecStart = utils.escapeSystemdExecArgs command;
        User = cfg.user;
        Group = cfg.group;
        Restart = "on-failure";
        RestartSec = 3;
        UMask = "0027";

        NoNewPrivileges = true;
        PrivateDevices = true;
        PrivateTmp = true;
        ProtectClock = true;
        ProtectControlGroups = true;
        ProtectKernelLogs = true;
        ProtectKernelModules = true;
        ProtectKernelTunables = true;
        ProtectSystem = "full";
        RestrictSUIDSGID = true;
      };
    };
  };
}
