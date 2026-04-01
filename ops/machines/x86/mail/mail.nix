{ config, ... }:

let
  release = "nixos-25.11";
in {
  imports = [
    (builtins.fetchTarball {
      url = "https://gitlab.com/simple-nixos-mailserver/nixos-mailserver/-/archive/${release}/nixos-mailserver-${release}.tar.gz";
      sha256 = "0pqc7bay9v360x2b7irqaz4ly63gp4z859cgg5c04imknv0pwjqw";
    })
  ];

  # temporary fix for the issue linked below that showed up after updating to
  # nixos-24.05 and the nixos-24.05 release
  # https://gitlab.com/simple-nixos-mailserver/nixos-mailserver/-/issues/275
  services.dovecot2.sieve.extensions = [ "fileinto" ];

  mailserver = {
    enable = true;
    fqdn = "mail.emile.space";
    domains = [ "emile.space" ];

    # A list of all login accounts. To create the password hashes, use
    # nix run nixpkgs.apacheHttpd -c htpasswd -nbB "" "super secret password" | cut -d: -f2
    loginAccounts = {
        "mail@emile.space" = {
            # hashedPasswordFile = "/etc/nixos/keys/mail";
            # hashedPasswordFile = config.age.secrets."mail_emile_space_password".path;

            # nix-shell -p mkpasswd --run 'mkpasswd -sm bcrypt'
            hashedPassword = "$2b$05$PlAPjtM6u6f/VKRDiQTnceJusqbEM9pjuRN34eEcw.nvBaZSa/yxa";

            aliases = ["@emile.space"];
        };
    };

    localDnsResolver = false;

    # Use Let's Encrypt certificates. Note that this needs to set up a stripped down nginx and opens port 80.
    #certificateScheme = 3;
    certificateScheme = "acme-nginx";

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
    stateVersion = 3;
  };
}
