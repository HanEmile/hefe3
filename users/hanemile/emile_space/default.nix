{ pkgs, ... }:

{
	a = pkgs.runCommand "emile.space" {} ''
		${pkgs.curl}/bin/curl "https://emile.space"
	'';
}
