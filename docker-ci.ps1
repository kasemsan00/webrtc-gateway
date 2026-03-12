$branch = "1.0.8"

docker build --push -t registry.kasemsan.com/gateway-frontend:$branch -f apps/frontend/Dockerfile .

docker build --push -t registry.kasemsan.com/k2-gateway:$branch -f apps/gateway/Dockerfile .

