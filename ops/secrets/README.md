# secrets

Edit like this:

```bash
; EDITOR=hx nix run git+https://github.com/ryantm/agenix -- -e <secret>
; ragenix --editor hx --edit lampadas_pinto_pike_ts_net_key.age
```

Rekey like this:

```bash
; cd ops/secrets
; nix run git+https://github.com/ryantm/agenix -- --rekey
; ragenix -r
```
