# Security Model

The Nexus Framework is built on the principle of **Least Privilege for Agents**. Agents should never hold the "keys to the kingdom" (Refresh Tokens); they should only hold short-lived "Usage Secrets."

## The 3-Key System

Security in Nexus relies on three primary environment variables that must be guarded with extreme care.

### 1. The Encryption Key (`ENCRYPTION_KEY`)
- **Type:** 32-byte Base64 encoded string.
- **Role:** Used for AES-GCM 256-bit encryption of tokens at rest in the PostgreSQL database.
- **Impact:** If compromised, an attacker can decrypt all stored Refresh Tokens. If lost, all existing connections are permanently broken.

### 2. The State Key (`STATE_KEY`)
- **Type:** 32-byte Base64 encoded string.
- **Role:** Used to sign and verify OIDC `state` and `nonce` parameters.
- **Impact:** Prevents CSRF (Cross-Site Request Forgery) and Replay Attacks during the handshake phase. Both the Broker and Gateway must use the same key.

### 3. The API Key (`API_KEY` / `BROKER_API_KEY`)
- **Role:** Authenticates the Gateway to the Broker and the Admin to the Broker.
- **Impact:** Controls access to provider registration and token retrieval.

## Usage Secrets vs. Master Secrets

Nexus enforces a strict boundary between the **Control Plane** and the **Data Plane**.

| Secret Type | Examples | Held By | Lifetime | Risk |
| :--- | :--- | :--- | :--- | :--- |
| **Master Secret** | Refresh Tokens, API Secrets | Broker (Vault) | Persistent | High (Account Takeover) |
| **Usage Secret** | Access Tokens, Signed Headers | Bridge (RAM) | Short (< 1hr) | Low (Temporary Access) |

### Protection Mechanism:
1.  The Broker retrieves the Master Secret (Refresh Token) from the database.
2.  It decrypts the secret and uses it to fetch a new Usage Secret (Access Token).
3.  It returns ONLY the Usage Secret to the Gateway and Bridge.
4.  If an Agent is compromised, the attacker only gains access to that single, short-lived Usage Secret. They cannot pivot to other connections or maintain long-term access.

## Network Hardening

### IP Allowlisting
The Broker supports an `ALLOWED_CIDRS` policy. In production, this should be restricted to the IP address of the **Nexus Gateway**. This ensures that even if an Admin Key is leaked, it cannot be used from outside your trusted network.

### mTLS (Roadmap)
Future versions of Nexus will support mutual TLS between the Gateway and Broker for cryptographically enforced identity beyond API keys.
