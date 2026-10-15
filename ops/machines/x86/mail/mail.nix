{ config, ... }:

let
  release = "nixos-26.05";
in {
  imports = [
    (builtins.fetchTarball {
      url = "https://gitlab.com/simple-nixos-mailserver/nixos-mailserver/-/archive/${release}/nixos-mailserver-${release}.tar.gz";
      sha256 = "0jicsxjg2c3vami08csrfccw6273yv12627ws8qx69lg5wz43abx";
    })
  ];

  mailserver = {
    enable = true;
    fqdn = "mail.emile.space";
    domains = [ "emile.space" ];

    # A list of all login accounts. To create the password hashes, use
    # nix run nixpkgs.apacheHttpd -c htpasswd -nbB "" "super secret password" | cut -d: -f2
    accounts = {
        "mail@emile.space" = {
            # nix-shell -p mkpasswd --run 'mkpasswd -sm bcrypt'
            hashedPassword = "$2b$05$PlAPjtM6u6f/VKRDiQTnceJusqbEM9pjuRN34eEcw.nvBaZSa/yxa";

            aliases = ["@emile.space"];
        };
    };

    # Local recursive resolver (kresd, 127.0.0.1:53) recommended when
    # running rspamd: public/DHCP-provided resolvers are blocked by
    # Spamhaus/URIBL/DNSWL, which made rspamd disable its IP/domain
    # blocklists and left spam scores below the reject threshold.
    localDnsResolver = true;

    # Cert comes from the ACME cert declared in ./default.nix
    # (security.acme.certs."mail.emile.space"); nginx on this host serves
    # the http-01 challenges.
    x509.useACMEHost = "mail.emile.space";

    # Enable IMAP and POP3
    enableImap = true;
    enablePop3 = true;
    enableSubmission = true;

    enableImapSsl = true;
    enablePop3Ssl = true;
    enableSubmissionSsl = true;

    enableManageSieve = true;

    virusScanning = false;

    # reporting
    dmarcReporting.enable = true;
    tlsrpt.enable = true;

    # abuse
    systemContact = "postmaster@emile.space";

    # https://nixos-mailserver.readthedocs.io/en/latest/migrations.html
    # stateVersion 5 directly: the 26.05 sieve migration (migration-05,
    # moving ManageSieve scripts from /var/sieve into the mail homes)
    # is vacuous here - /var/sieve on mail is empty (verified 2026-10-15),
    # so there is nothing to move. Migrations #2/#4 are LDAP-only.
    stateVersion = 5;
  };
}
