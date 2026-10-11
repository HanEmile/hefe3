# TROUBLESHOOT

## Nix on macos having cert problems

```
sudo sh -c 'echo "ssl-cert-file = /etc/ssl/cert.pem" >> /etc/nix/nix.conf'
sudo launchctl unload /Library/LaunchDaemons/org.nixos.nix-daemon.plist
sudo launchctl load /Library/LaunchDaemons/org.nixos.nix-daemon.plist
```
