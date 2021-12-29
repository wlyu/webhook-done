#!/usr/bin/env bash
ca_subj="/CN=webhook02"
svc="webhook02.infra.svc"
# Generate the CA cert and private key
openssl req -nodes -new -x509 -keyout ca.key -out ca.crt -subj $ca_subj  -days 3650
# Generate the private key for the webhook server
openssl genrsa -out webhook-server-tls.key 2048
# Generate a Certificate Signing Request (CSR) for the private key, and sign it with the private key of the CA.
openssl req -new -key webhook-server-tls.key -subj "/CN=$svc" \
| openssl x509 -req -CA ca.crt -extfile <(printf "subjectAltName=DNS:$svc") \
-CAkey ca.key -CAcreateserial -out webhook-server-tls.crt -days 3650
kubectl create secret tls webhook-server-tls-secret \
    --cert "webhook-server-tls.crt" \
    --key "webhook-server-tls.key" \
    -n infra
# Read the PEM-encoded CA certificate, base64 encode it, and replace the `${CA_PEM_B64}` placeholder in the YAML
# template with it. Then, create the Kubernetes resources.
ca_pem_b64="$(openssl base64 -A <"ca.crt")"
sed -e 's@${CA_PEM_B64}@'"$ca_pem_b64"'@g' deplyment.yaml