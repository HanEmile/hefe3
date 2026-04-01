while true;
do
  curl -m 1 -L --cacert certs/HITM-Proxy-cert.pem --proxy-cacert certs/HITM-Proxy-cert.pem -x https://127.0.0.1:8443 https://emile.space
  sleep 0.1
done
