# hitm

hacker in the middle

A simple yet powerful tool for intercepting and modifying HTTP traffic.

## Proxy

## Frontend

* `/`: GET the webinterface
* `/static/`: GET static files
* `/repeat`: POST requests to repeat
* `/flow`: GET flow `?id=<id>`
* `/events`: GET events via SSE
* `/history`: GET history
* `/intercept`: POST enable intercept mode
* `/forward`: POST forward requests `?id=<id>`
* `/drop`: POST drop requests `?id=<id>`

## Certs

```bash
$ openssl x509 -noout -in certs/0-0-0-0-8086-cert.pem -text
```

## tests

Proxy with itself:

```
curl -v -X POST --cacert ./certs/HITM-Proxy-CA-ca-cert.pem --proxy-cacert ./certs/HITM-Proxy-CA-ca-cert.pem --proxy "https://192.168.178.30:9002" "https://192.168.178.30:8086/newListener?address=192.168.178.30:9002?type=https"
```
