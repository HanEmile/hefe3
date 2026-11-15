---
title: mail.emile.space
type: vps
visibility: internal
hostname: mail.emile.space
storage: ext4
services: mailserver, prometheus
domain: emile.space
location: cloud
---

# mail.emile.space

## VMs

(children "mail-server" :via "part-of" :show (:title :ip-default :ip-private :memory :services))
