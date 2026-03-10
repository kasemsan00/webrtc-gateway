$branch = git rev-parse --abbrev-ref HEAD
docker build --push -t registry.kasemsan.com/k2-gateway:$branch .