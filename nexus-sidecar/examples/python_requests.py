"""Call a provider API through nexus-sidecar without handling provider tokens.

Run with:
    NEXUS_SIDECAR_URL=http://localhost:8070 \
    NEXUS_CONNECTION_ID=<connection_id> \
    python examples/python_requests.py
"""

import os

import requests


sidecar_url = os.getenv("NEXUS_SIDECAR_URL", "http://localhost:8070")
connection_id = os.environ["NEXUS_CONNECTION_ID"]

response = requests.get(
    f"{sidecar_url}/user/repos",
    headers={
        "X-Nexus-Provider": "github",
        "X-Nexus-Connection-ID": connection_id,
    },
    timeout=30,
)
response.raise_for_status()
print(response.json())
