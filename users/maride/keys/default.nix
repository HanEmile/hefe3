{ ... }:

let
  # merges the list of keys below with a list containing a key "all" with
  # the value being a list of all key values
  # src: the tvl monorepo, //users/tazjin/keys/default.nix as of 2025-10-07
  withAll = keys: keys // { all = builtins.attrValues keys; };
in
withAll {
  # fetched from github on `2025-11-19 16:41+UTC`
  a = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDIe1DRCN0Q0g/lxHOuoU4taSqq8+tz0AUZPt9Tmn1bgTUiSyHw0QqqCMm8JFejOJO6T6l5UJVXC8kQy00nJgWufB4YawOAGIWRT3+TqlnLo8lJIWJMz0yhx4Gs9hpWoPmNFt7fDo0Ic5u7yg73n43GIQzsGJFNBEeMK44je42d+eXxhvOZZDyhl8zAfc1zb2fdfKhJAIEyqVcNbL0h8nzfe5lGRjkkotT1Jcgi/O5t3Y7tzY2YW/4pN4c/lzw8rb5vwDFtiSyhoF9em8OClCexLOZbcevntAdXmV8Ulun5XbnEM7GziOk3mauHg41eUGmS2/3e4slltLfiP9+/U+C1";
  b = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICDREdd9oGq26+6YhS9MmBkVfdytYvwL+lSGHftnzXE+";
  c = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILx2kp4JDm+Y8Dg5C+4iWm6KAcsP7g4O9p7q6Dmaride";
}
