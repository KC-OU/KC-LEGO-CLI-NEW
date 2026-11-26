# DRAFT NixOS configuration for the KC-Parts server. NOT TESTED: nothing here has been built or booted.
# Written from the live Arch box's real unit files and package list (see docs/guides/migration-runbook.md for how
# to use it). Read every TODO before trusting it. Secrets are never put in this file: they live in /root/.config
# and the restored docker-server/ tree, copied in from the encrypted export.
{ config, pkgs, ... }:
{
  # TODO: hardware-configuration.nix and the bootloader depend on the new VPS provider's image.
  imports = [ ./hardware-configuration.nix ];
  networking.hostName = "kc-server-1";
  time.timeZone = "UTC";
  system.stateVersion = "25.05"; # TODO: set to the release you actually install

  # ---- SSH: public, password login as today. Restore the OLD host keys from the export first so clients do not
  # see a changed fingerprint (runbook step 5). TODO: move to keys-only once a key is installed and tested.
  services.openssh = {
    enable = true;
    settings = { PermitRootLogin = "yes"; PasswordAuthentication = true; };
  };

  # ---- Firewall. NixOS's firewall does not protect Docker-published ports (Docker writes its own rules), so the
  # same DOCKER-USER rules as /usr/local/bin/wms-firewall.sh are added below. Only 22 is open to the internet;
  # everything else arrives through the Cloudflare tunnel on 127.0.0.1.
  networking.firewall = {
    enable = true;
    allowedTCPPorts = [ 22 ];
    extraCommands = ''
      for p in 80 20011 3000 3002 8081 8082 9091; do
        iptables -C DOCKER-USER -i eth0 -p tcp -m conntrack --ctstate DNAT --ctorigdstport $p -j DROP 2>/dev/null \
          || iptables -I DOCKER-USER -i eth0 -p tcp -m conntrack --ctstate DNAT --ctorigdstport $p -j DROP || true
      done
    '';
  };

  virtualisation.docker.enable = true; # compose stacks are restored under /root/docker-server and started with `docker compose up -d`

  environment.systemPackages = with pkgs; [
    git gh go rustc cargo gcc sqlite tmux ttyd age openssl zstd python3 curl jq shellcheck cloudflared
    # TODO: `cf` (Cloudflare CLI), `staticcheck`, `govulncheck`, `gosec`, `cargo-audit` come from `go install` / `cargo install`
  ];

  # ---- The Go gateway (telnet + web terminal + dashboard API). The binary is built from the restored repo
  # (runbook step 7) into /usr/local/bin/wms-go, exactly as scripts/deploy.sh does today.
  systemd.services.wms-gateway = {
    description = "ModernWMS & PartDB Go Suite - Telnet + Web Terminal Gateway";
    after = [ "network.target" "docker.service" ];
    wantedBy = [ "multi-user.target" ];
    path = [ pkgs.tmux pkgs.ttyd pkgs.git ];
    serviceConfig = {
      ExecStart = "/usr/local/bin/wms-go gateway serve";
      WorkingDirectory = "/root/modernwms-partdb-go";
      Restart = "always"; RestartSec = 3;
    };
  };

  # ---- The Rust dashboard.
  systemd.services.kc-parts-dashboard = {
    description = "KC-Parts dashboard (Rust)";
    after = [ "network.target" ];
    wantedBy = [ "multi-user.target" ];
    serviceConfig = {
      WorkingDirectory = "/root/kc-parts-dashboard-rs";
      EnvironmentFile = "/root/kc-parts-dashboard-rs/.env";
      ExecStart = "/root/kc-parts-dashboard-rs/target/release/kc_parts_dashboard";
      Restart = "on-failure"; RestartSec = 2;
    };
  };

  # ---- Nightly encrypted backups (local + private GitHub release) and the failure alert.
  systemd.services.wms-backup = {
    description = "ModernWMS database backup";
    path = [ pkgs.gh pkgs.git ];
    unitConfig.OnFailure = "wms-backup-failed.service";
    serviceConfig = { Type = "oneshot"; ExecStart = "/usr/local/bin/wms-backup-run.sh"; };
  };
  systemd.services.wms-backup-failed = {
    description = "Alert that the nightly backup failed";
    serviceConfig = { Type = "oneshot"; ExecStart = ''/usr/local/bin/wms-go notify send backup "KC-PARTS backup FAILED" "Run: journalctl -u wms-backup.service"''; };
  };
  systemd.timers.wms-backup = {
    wantedBy = [ "timers.target" ];
    timerConfig = { OnCalendar = "*-*-* 03:00:00"; RandomizedDelaySec = "15min"; Persistent = true; };
  };

  # ---- Optional: loopback-only public stock lookup (publish by adding a Cloudflare tunnel route to 127.0.0.1:8095).
  # systemd.services.wms-public-stock = {
  #   wantedBy = [ "multi-user.target" ]; after = [ "network.target" ];
  #   serviceConfig = { ExecStart = "/usr/local/bin/wms-go public-stock"; Restart = "always"; DynamicUser = false; };
  # };

  # ---- Still TODO, deliberately not guessed: sshd-log-feed.service + CrowdSec bouncer (copy the unit files from the
  # export's systemd/ directory), the /var/log/wms-terminal tmpfiles rule, and the cloudflared container (host network).
  systemd.tmpfiles.rules = [ "d /var/log/wms-terminal 0700 root root 30d" "d /var/log/crowdsec-feed 0755 root root -" ];
}
