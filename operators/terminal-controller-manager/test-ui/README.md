# Terminal Controller Test UI

A minimal test UI for testing the terminal controller against a local Kind cluster.

## Prerequisites

1. Running Kind cluster with platform-mesh deployed
2. Terminal controller deployed and running
3. A Terminal CR created in a kcp workspace
4. An OIDC token (from Keycloak)

## Usage

### 1. Start a local web server

```bash
# From this directory
npx serve .

# Or with Python
python3 -m http.server 3000

# Or just open index.html directly in browser (file://)
```

### 2. Port-forward the terminal controller

```bash
kubectl port-forward svc/terminal-controller-manager 8080:8080 -n platform-mesh-system
```

### 3. Create a Terminal CR

```bash
# Apply a test terminal resource in your kcp workspace
kubectl apply -f - <<EOF
apiVersion: terminal.platform-mesh.io/v1alpha1
kind: Terminal
metadata:
  name: test-terminal
spec:
  target:
    workspacePath: "root:myorg:myworkspace"
    apiServerURL: "https://kcp.example.com"
  host:
    namespace: "terminal-sessions"
EOF
```

### 4. Get an OIDC token

From browser DevTools (if already logged into platform-mesh):
1. Open DevTools (F12)
2. Go to Application > Local Storage
3. Find the access_token or id_token

Or via curl:
```bash
# Example for Keycloak
curl -X POST "https://keycloak.example.com/realms/platform-mesh/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=password" \
  -d "client_id=platform-mesh-ui" \
  -d "username=your-user" \
  -d "password=your-password" | jq -r '.access_token'
```

### 5. Connect

1. Open the test UI in your browser (http://localhost:3000)
2. Enter the WebSocket URL: `ws://localhost:8080/ws/test-terminal`
3. Paste your OIDC token
4. Click "Connect"

## Troubleshooting

### Connection refused
- Check if port-forward is running
- Check if terminal controller is deployed

### Token errors
- Token may be expired, get a fresh one
- Check if token is valid for the target kcp workspace

### Terminal not ready
- Check Terminal CR status: `kubectl get terminal test-terminal -o yaml`
- Check if terminal pod is running: `kubectl get pods -n terminal-sessions`
