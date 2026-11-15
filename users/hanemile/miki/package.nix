{ lib, buildGoModule }:

buildGoModule {
  pname = "miki";
  version = "0.0.1";

  src = ./.;

  # this hash is updated from the example, which seems to be out of date
  vendorHash = "sha256-Xv4CTILTM7Ys4nlB1CKgrQxUJj4LQz23k9v9dWnhHYE=";

  meta = {
    description = "Minimal emile.space styled wiki with a git backend";
    homepage = "https://github.com/hanemile/hefe3";
    license = lib.licenses.mit;
    maintainers = with lib.maintainers; [ hanemile ];
  };
}
