# TLS certificate files

Place the production certificate chain at `fullchain.pem` and the matching
private key at `privkey.pem`. The private key must not be committed or copied
back into the original cloud-control directory.

Start the TLS deployment with both Compose files:

```powershell
docker compose -f docker-compose.yml -f docker-compose.tls.yml up -d
```
