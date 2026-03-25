$branch = $env:BRANCH ?? "1.1.1"
$registry = $env:REGISTRY ?? "registry.kasemsan.com"

docker build --push `
  -t $registry/gateway-frontend:$branch `
  --build-arg VITE_GATEWAY_URL=$env:VITE_GATEWAY_URL `
  --build-arg VITE_TURN_URL=$env:VITE_TURN_URL `
  --build-arg VITE_TURN_USERNAME=$env:VITE_TURN_USERNAME `
  --build-arg VITE_TURN_CREDENTIAL=$env:VITE_TURN_CREDENTIAL `
  --build-arg VITE_KEYCLOAK_URL=$env:VITE_KEYCLOAK_URL `
  --build-arg VITE_KEYCLOAK_REALM=$env:VITE_KEYCLOAK_REALM `
  --build-arg VITE_KEYCLOAK_CLIENT=$env:VITE_KEYCLOAK_CLIENT `
  --build-arg VITE_CONFIG_AUTORECORD=$env:VITE_CONFIG_AUTORECORD `
  -f apps/frontend/Dockerfile .

docker build --push -t $registry/k2-gateway:$branch -f apps/gateway/Dockerfile .

