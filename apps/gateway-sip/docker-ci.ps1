param(
	[string]$Tag = "1.0.2"
)

Write-Host "Building and pushing registry.kasemsan.com/gateway-sip:$Tag"
docker build --push -t registry.kasemsan.com/gateway-sip:$Tag .