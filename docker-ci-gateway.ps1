$branch = "1.0.6"
docker build --push -t registry.kasemsan.com/k2-gateway:$branch -f apps/gateway-sip/Dockerfile apps/gateway-sip
