while true;
do
  curl \
    -k \
    -m 1 \
    -L \
    --cacert certs/HITM-Proxy-CA-ca-cert.pem \
    --proxy-cacert certs/HITM-Proxy-Ca-ca-cert.pem \
    -x https://127.0.0.1:9002 \
    "https://0.0.0.0:8080/"

  sleep 0.1
done
