# secrets

Edit like this:

```bash
; EDITOR=hx nix run git+https://github.com/ryantm/agenix -- -e <secret>
```

Rekey like this:

```bash
; cd ops/secrets
; nix run git+https://github.com/ryantm/agenix -- --rekey
```
